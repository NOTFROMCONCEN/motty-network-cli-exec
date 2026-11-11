package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"motty/internal/netguard"
)

// The fork publishes its own builds, so it checks its own releases instead of
// the upstream mirror. Each release carries a latest.json asset in the mirror's
// format; the package itself is a GitHub release asset.
//
// This file is fork-only: it is new here and upstream has nothing like it, so a
// rebase cannot conflict with it. The only upstream file it needs is Check.
const (
	// ForkRepo is where the fork publishes its releases.
	ForkRepo = "NOTFROMCONCEN/motty-network-cli-exec"
	// ForkFeed is the newest release of that repository.
	ForkFeed = "https://api.github.com/repos/" + ForkRepo + "/releases/latest"
	// ForkManifest is the asset carrying sizes, checksums and signatures.
	ForkManifest = "latest.json"
	// ForkBase is the upstream release this fork's version line sits on, and
	// ForkCodename names this era of the fork. A build's machine-facing
	// version is the base plus a build counter — 0.3.1.4 on base 0.3.1 — so
	// the numbers themselves say which upstream release a build carries, and
	// the counter moves on top of it until upstream publishes its next
	// version, when the base moves and a new codename may take over.
	ForkBase     = "0.4.0"
	ForkCodename = "Genshin"
)

// forkFeedURL is the newest release of the fork, on GitHub's API. It is a
// variable so a test can serve it locally, the way release signing does.
var forkFeedURL = ForkFeed

// The manifest is mirrored on the feed branch: one branch, one file, kept at
// the newest release by the publishing script. CDNs serve it from hosts that
// answer where github.com refuses connections, and a mirror that is behind
// only offers an older release, which is no harm — the caller compares
// versions anyway.
var feedMirrors = []string{
	"https://cdn.jsdelivr.net/gh/" + ForkRepo + "@feed/" + ForkManifest,
	"https://fastly.jsdelivr.net/gh/" + ForkRepo + "@feed/" + ForkManifest,
	"https://testingcf.jsdelivr.net/gh/" + ForkRepo + "@feed/" + ForkManifest,
}

// forkTagPattern is the tag a fork release carries: the upstream base it was
// built on, a netcli marker, a build counter and the era's codename, as in
// v0.3.1-netcli-build.4-arknights.
var forkTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-netcli-build\.(\d+)-([a-z0-9]+)$`)

// TagFor names a build the way the fork publishes it: the upstream base, the
// marker, a build counter and the codename — v0.3.1-netcli-build.4-arknights
// for 0.3.1.4. The counter is the version's own last number, so the name and
// the machine-readable version move together. NSIS and the update comparison
// only take plain dotted versions, which is why the name is a layer above
// them, not a replacement. A version from another line keeps a plain tag.
//
// A codename that cannot live in a tag — spaces, punctuation, non-ASCII — or
// a non-canonical version spelling ("0.03.8.1") has no name either: both
// would publish a tag the feed can never read back, silently breaking every
// client's update check, so the caller sees a plain tag and forkrelease
// refuses to publish it.
func TagFor(version, base, codename string) string {
	v, okV := parse(version)
	b, okB := parse(base)
	if !okV || !okB || v[0] != b[0] || v[1] != b[1] || v[2] != b[2] || v[3] <= 0 {
		return "v" + version
	}
	if canonical := fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3]); canonical != version {
		return "v" + version
	}
	name := strings.ToLower(codename)
	if !codenamePattern.MatchString(name) {
		return "v" + version
	}
	return fmt.Sprintf("v%s-netcli-build.%d-%s", base, v[3], name)
}

// codenamePattern is the era name as a tag carries it: lower-case letters and
// digits only, the same class forkTagPattern reads back.
var codenamePattern = regexp.MustCompile(`^[a-z0-9]+$`)

// VersionFromTag reads a tag the fork publishes back into the dotted version
// the update machinery compares: v0.3.1-netcli-build.4-arknights is 0.3.1.4. A
// plain numbered tag keeps working, so releases published before this naming
// stay readable.
func VersionFromTag(tag string) (string, bool) {
	if match := forkTagPattern.FindStringSubmatch(tag); match != nil {
		version := match[1] + "." + match[2] + "." + match[3] + "." + match[4]
		if _, ok := parse(version); ok {
			return version, true
		}
		return "", false
	}
	version := strings.TrimPrefix(tag, "v")
	if _, ok := parse(version); !ok {
		return "", false
	}
	return version, true
}

// checkFork is Check's implementation: GitHub's feed and manifest first, then
// the CDN mirrors of the feed branch, then — worst case — the release the feed
// itself described, with its page and no package.
func checkFork(ctx context.Context, client *http.Client, goos, goarch string) (Release, error) {
	client = feedClient(client)
	var fallback Release
	var haveFallback bool
	var lastErr error
	release, tag, manifestURL, err := forkRelease(ctx, client, forkFeedURL)
	if err == nil {
		full, manifestErr := manifestRelease(ctx, client, manifestURL, tag, release.Version, release.Notes, ForkRepo, goos, goarch)
		if manifestErr == nil {
			return full, nil
		}
		// The feed named a release whose manifest did not arrive. Keep it for
		// the worst case and see whether a mirror has the manifest.
		fallback, haveFallback = release, true
		lastErr = manifestErr
	} else {
		lastErr = err
	}
	for _, mirror := range feedMirrors {
		full, mirrorErr := mirrorRelease(ctx, client, mirror, ForkRepo, goos, goarch)
		if mirrorErr == nil {
			// A mirror is only a copy. When the feed itself already named a
			// release, a mirror's answer must not be older than that: a stale
			// mirror pinning every client to the previous version is worse
			// than the feed's page-only answer, which at least tells the
			// truth about what was released.
			if haveFallback && !Newer(fallback.Version, full.Version) && full.Version != fallback.Version {
				return fallback, nil
			}
			return full, nil
		}
		lastErr = mirrorErr
	}
	if haveFallback {
		return fallback, nil
	}
	if errors.Is(lastErr, ErrNoRelease) {
		return Release{}, ErrNoRelease
	}
	return Release{}, lastErr
}

// feedClient bounds a feed read: the answers are small, and a host that takes
// the connection and then stalls must not hold the check for long.
func feedClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// manifestFile is what a release's manifest says: the build it names, the tag
// it was published under, and one entry per package with what a download is
// held to.
type manifestFile struct {
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Source  string `json:"source"`
	Notes   string `json:"notes"`
	Files   []struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
		Signature string `json:"signature"`
	} `json:"files"`
}

// manifestRelease reads a manifest that describes a known release: the feed
// already said which version and tag it belongs to, so a manifest that names
// another one is a stale copy, not this release.
func manifestRelease(ctx context.Context, client *http.Client, manifestURL, tag, version, feedNotes, repo, goos, goarch string) (Release, error) {
	var manifest manifestFile
	if err := readJSON(ctx, client, manifestURL, &manifest); err != nil {
		return Release{}, err
	}
	if manifest.Version != version || manifest.Tag != tag {
		return Release{}, errors.New("update: the release manifest does not describe this release")
	}
	release, err := releaseFromManifest(version, tag, &manifest, repo, goos, goarch)
	if err != nil {
		return Release{}, err
	}
	if release.Notes == "" {
		release.Notes = cutUTF8(feedNotes, maxNotesBytes)
	}
	return release, nil
}

// mirrorRelease reads the manifest as it is mirrored on the feed branch, where
// no release feed answers: the manifest itself names the version and tag, and
// both have to be a shape this fork publishes. A mirror that is behind only
// offers an older release, which the caller's version comparison rejects.
//
// A mirror is not the publisher, so the page and the notes the manifest
// carries never reach the user from it: a CDN this project does not control
// must not be able to put its own address or its own markdown in front of the
// update dialog. Both are pinned to the repository the manifest belongs to;
// notes only come from the release feed itself.
func mirrorRelease(ctx context.Context, client *http.Client, manifestURL, repo, goos, goarch string) (Release, error) {
	var manifest manifestFile
	if err := readJSON(ctx, client, manifestURL, &manifest); err != nil {
		return Release{}, err
	}
	version, ok := VersionFromTag(strings.TrimSpace(manifest.Tag))
	if !ok || manifest.Version != version {
		return Release{}, errors.New("update: the mirrored manifest has no usable version")
	}
	release, err := releaseFromManifest(version, strings.TrimSpace(manifest.Tag), &manifest, repo, goos, goarch)
	if err != nil {
		return Release{}, err
	}
	release.URL = "https://github.com/" + repo + "/releases/tag/" + strings.TrimSpace(manifest.Tag)
	release.Notes = ""
	return release, nil
}

// releaseFromManifest turns a manifest into the release it describes. The
// download address is built from repo and the tag, never taken from the
// manifest itself, so a tampered feed cannot send the download to another
// host; the checksum and the release signature still hold the bytes.
func releaseFromManifest(version, tag string, manifest *manifestFile, repo, goos, goarch string) (Release, error) {
	if !strings.HasPrefix(manifest.Source, "https://") {
		return Release{}, errors.New("update: the release page is not an https address")
	}
	// The manifest lists every package it publishes, so the answer has checked
	// what this platform can get — even when the list names nothing for it.
	// A file that names the platform but carries nothing a download could be
	// held to is the one unknown: claiming the release package-less would be
	// as wrong as a page-only answer claiming it.
	release := Release{Version: version, URL: manifest.Source, Notes: cutUTF8(manifest.Notes, maxNotesBytes), PlatformPackageChecked: true}
	stable := StableName(goos, goarch)
	if stable == "" {
		return release, nil
	}
	name := strings.Replace(stable, "motty-", "motty-"+version+"-", 1)
	for _, file := range manifest.Files {
		if file.Name != name {
			continue
		}
		if !sha256Pattern.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxAssetBytes {
			release.PlatformPackageChecked = false
			break
		}
		pinned := "https://github.com/" + repo + "/releases/download/" + tag + "/" + name
		release.Asset = Asset{Name: name, URL: pinned, SHA256: file.SHA256, Size: file.Size, Signature: validSignature(file.Signature)}
		break
	}
	return release, nil
}

// LatestFork reads the newest release of repo and the manifest attached to it.
// The package URL is built from repo and the tag, never taken from either
// answer, so a feed that was tampered with cannot send the download to another
// host. A release without a manifest or without a package for the platform
// still answers: the release page is all there is, and the caller offers that.
func LatestFork(ctx context.Context, client *http.Client, feed, repo, goos, goarch string) (Release, error) {
	client = feedClient(client)
	release, tag, manifestURL, err := forkRelease(ctx, client, feed)
	if err != nil {
		return Release{}, err
	}
	stable := StableName(goos, goarch)
	if stable == "" || manifestURL == "" {
		return release, nil
	}
	var manifest manifestFile
	if err := readJSON(ctx, client, manifestURL, &manifest); err != nil {
		return Release{}, err
	}
	// The manifest has to describe the release it came with, or it is a stale
	// copy left attached to a newer tag.
	if manifest.Version != release.Version || manifest.Tag != tag {
		return Release{}, errors.New("update: the release manifest does not describe this release")
	}
	return releaseFromManifest(release.Version, tag, &manifest, repo, goos, goarch)
}

// forkRelease reads the release feed and returns what it says: the release, its
// tag and the address of its manifest asset, empty when it has none.
func forkRelease(ctx context.Context, client *http.Client, feed string) (Release, string, string, error) {
	if !strings.HasPrefix(feed, "https://") {
		return Release{}, "", "", errors.New("update: the release feed is not an https address")
	}
	var body struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Notes      string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := readJSON(ctx, client, feed, &body); err != nil {
		return Release{}, "", "", err
	}
	tag := strings.TrimSpace(body.Tag)
	version, ok := VersionFromTag(tag)
	// The tag names the download directory, so it has to be one this fork
	// publishes: its own tagged builds or a plain numbered one.
	if !ok || body.Draft || body.Prerelease {
		return Release{}, "", "", errors.New("update: the release feed has no usable version")
	}
	if !strings.HasPrefix(body.URL, "https://") {
		return Release{}, "", "", errors.New("update: the release page is not an https address")
	}
	release := Release{Version: version, URL: body.URL, Notes: cutUTF8(body.Notes, maxNotesBytes)}
	for _, asset := range body.Assets {
		if asset.Name != ForkManifest || !strings.HasPrefix(asset.URL, "https://") {
			continue
		}
		return release, tag, asset.URL, nil
	}
	return release, tag, "", nil
}

// readJSON fetches one JSON answer into into. A 404 is ErrNoRelease, which is
// also what a private repository looks like to an anonymous request.
func readJSON(ctx context.Context, client *http.Client, address string, into any) error {
	if err := netguard.CheckURL(address); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "motty/"+Current)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrNoRelease
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("update: the release feed answered %s", response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxFeedBytes)).Decode(into); err != nil {
		return fmt.Errorf("update: reading the release feed: %w", err)
	}
	return nil
}
