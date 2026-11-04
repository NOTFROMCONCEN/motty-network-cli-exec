package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
)

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
	version := strings.TrimPrefix(tag, "v")
	// The tag names the download directory, so it has to be a plain version:
	// nothing else is allowed to reach a URL the app builds.
	if !tagPattern.MatchString(tag) {
		return Release{}, "", "", errors.New("update: the release feed has no usable version")
	}
	if _, ok := parse(version); !ok || body.Draft || body.Prerelease {
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
