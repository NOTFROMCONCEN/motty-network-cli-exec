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
	// ForkCodename names this era of the fork, until upstream publishes its
	// next version. The fork's major and minor never exceed the base's, so a
	// build of this fork never outranks upstream's own line.
	ForkBase     = "0.3.0"
	ForkCodename = "Arknights"
)

// forkTagPattern is the tag a fork release carries: the upstream base it was
// built on, a netcli marker, a build counter and the era's codename, as in
// v0.3.0-netcli-build.1-arknights.
var forkTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-netcli-build\.(\d+)-([a-z0-9]+)$`)

// TagFor names a build the way the fork publishes it: the upstream base, the
// marker, a build counter and the codename — v0.3.0-netcli-build.1-arknights
// for 0.3.1. The counter is the version's own patch number, so the name and
// the machine-readable version move together. NSIS and the update comparison
// only take plain dotted versions, which is why the name is a layer above
// them, not a replacement. A version from another line keeps a plain tag.
func TagFor(version, base, codename string) string {
	v, okV := parse(version)
	b, okB := parse(base)
	if !okV || !okB || v[0] != b[0] || v[1] != b[1] || v[2] <= 0 {
		return "v" + version
	}
	return fmt.Sprintf("v%s-netcli-build.%d-%s", base, v[2], strings.ToLower(codename))
}

// VersionFromTag reads a tag the fork publishes back into the dotted version
// the update machinery compares: v0.3.0-netcli-build.2-arknights is 0.3.2. A
// plain numbered tag keeps working, so releases published before this naming
// stay readable.
func VersionFromTag(tag string) (string, bool) {
	if match := forkTagPattern.FindStringSubmatch(tag); match != nil {
		version := match[1] + "." + match[2] + "." + match[4]
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

// LatestFork reads the newest release of repo and the manifest attached to it.
// The package URL is built from repo and the tag, never taken from either
// answer, so a feed that was tampered with cannot send the download to another
// host. A release without a manifest or without a package for the platform
// still answers: the release page is all there is, and the caller offers that.
func LatestFork(ctx context.Context, client *http.Client, feed, repo, goos, goarch string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	release, tag, manifestURL, err := forkRelease(ctx, client, feed)
	if err != nil {
		return Release{}, err
	}
	stable := StableName(goos, goarch)
	if stable == "" || manifestURL == "" {
		return release, nil
	}
	var manifest struct {
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
	if err := readJSON(ctx, client, manifestURL, &manifest); err != nil {
		return Release{}, err
	}
	// The manifest has to describe the release it came with, or it is a stale
	// copy left attached to a newer tag.
	if manifest.Version != release.Version || manifest.Tag != tag {
		return Release{}, errors.New("update: the release manifest does not describe this release")
	}
	if !strings.HasPrefix(manifest.Source, "https://") {
		return Release{}, errors.New("update: the release page is not an https address")
	}
	release.Notes = cutUTF8(manifest.Notes, maxNotesBytes)
	name := strings.Replace(stable, "motty-", "motty-"+release.Version+"-", 1)
	for _, file := range manifest.Files {
		if file.Name != name {
			continue
		}
		if !sha256Pattern.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxAssetBytes {
			break
		}
		pinned := "https://github.com/" + repo + "/releases/download/" + tag + "/" + name
		release.Asset = Asset{Name: name, URL: pinned, SHA256: file.SHA256, Size: file.Size, Signature: validSignature(file.Signature)}
		break
	}
	return release, nil
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
