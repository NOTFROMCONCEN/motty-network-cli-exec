package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const forkRepo = "owner/repo"

// forkServer serves a release feed at /latest and a manifest at
// /manifest.json. body builds the feed JSON and receives the manifest's
// address, which changes with the server's port.
func forkServer(t *testing.T, body func(manifestURL string) string, manifest string) *httptest.Server {
	t.Helper()
	var manifestURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprint(w, body(manifestURL))
		case "/manifest.json":
			fmt.Fprint(w, manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	manifestURL = server.URL + "/manifest.json"
	return server
}

// releaseWithAssets is the feed of a release that attaches the manifest.
func releaseWithAssets(tag string) func(string) string {
	return func(manifestURL string) string {
		return fmt.Sprintf(`{"tag_name":%q,"html_url":"https://github.com/owner/repo/releases/tag/%s","assets":[{"name":"latest.json","browser_download_url":%q}]}`,
			tag, tag, manifestURL)
	}
}

func noAssets(string) string {
	return `{"tag_name":"v0.3.1","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1","assets":[]}`
}

const goodSignature = "c2lnbmF0dXJl" // any base64: this package parses signatures, it does not verify them

func goodManifest(version, file string, size int64, sum string) string {
	return fmt.Sprintf(`{"version":%q,"tag":"v%s","source":"https://github.com/owner/repo/releases/tag/v%s","notes":"notes","files":[{"name":%q,"size":%d,"sha256":%q,"signature":%q}]}`,
		version, version, version, file, size, sum, goodSignature)
}

const setupName = "motty-0.3.1-windows-amd64-setup.exe"

func TestLatestForkReadsTheReleaseAndItsManifest(t *testing.T) {
	server := forkServer(t, releaseWithAssets("v0.3.1"), goodManifest("0.3.1", setupName, 42, goodHash))
	release, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "0.3.1" || release.Notes != "notes" {
		t.Fatalf("release %+v", release)
	}
	want := "https://github.com/owner/repo/releases/download/v0.3.1/" + setupName
	if release.Asset.Name != setupName || release.Asset.URL != want ||
		release.Asset.Size != 42 || release.Asset.SHA256 != goodHash || release.Asset.Signature != goodSignature {
		t.Fatalf("asset %+v", release.Asset)
	}
}

func TestLatestForkLeavesWhatItCannotUse(t *testing.T) {
	for name, test := range map[string]struct {
		body     func(string) string
		manifest string
	}{
		"no manifest attached": {noAssets, ""},
		"no package for the platform": {releaseWithAssets("v0.3.1"),
			goodManifest("0.3.1", "motty-0.3.1-darwin-arm64.zip", 42, goodHash)},
		"bad checksum": {releaseWithAssets("v0.3.1"), goodManifest("0.3.1", setupName, 42, "not-a-sum")},
		"no size":      {releaseWithAssets("v0.3.1"), goodManifest("0.3.1", setupName, 0, goodHash)},
		"plain http manifest": {func(string) string {
			return `{"tag_name":"v0.3.1","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1","assets":[{"name":"latest.json","browser_download_url":"http://example.com/latest.json"}]}`
		}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			server := forkServer(t, test.body, test.manifest)
			release, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64")
			if err != nil || release.Version != "0.3.1" || release.Asset.Has() {
				t.Fatalf("release %+v, error %v", release, err)
			}
		})
	}
}

func TestLatestForkRefusesWhatItCannotTrust(t *testing.T) {
	for name, body := range map[string]string{
		"not a version":   `{"tag_name":"nightly","html_url":"https://github.com/owner/repo/releases/tag/nightly","assets":[]}`,
		"a suffix":        `{"tag_name":"v0.3.1-rc1","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1-rc1","assets":[]}`,
		"a draft":         `{"tag_name":"v0.3.1","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1","draft":true,"assets":[]}`,
		"a prerelease":    `{"tag_name":"v0.3.1","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1","prerelease":true,"assets":[]}`,
		"plain http page": `{"tag_name":"v0.3.1","html_url":"http://example.com/r","assets":[]}`,
		"not json":        `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			server := forkServer(t, func(string) string { return body }, "")
			if _, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64"); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestLatestForkRefusesAManifestForAnotherRelease(t *testing.T) {
	for name, manifest := range map[string]string{
		"another version": goodManifest("0.3.2", "motty-0.3.2-windows-amd64-setup.exe", 42, goodHash),
		"another tag":     `{"version":"0.3.1","tag":"v0.3.0","source":"https://github.com/owner/repo/releases/tag/v0.3.1","files":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := forkServer(t, releaseWithAssets("v0.3.1"), manifest)
			if _, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64"); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestLatestForkReportsAMissingRelease(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()
	if _, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("a 404 gave %v, want ErrNoRelease", err)
	}
}

func TestLatestForkRefusesAPlainHTTPFeed(t *testing.T) {
	if _, err := LatestFork(context.Background(), nil, "http://example.com/latest", forkRepo, "windows", "amd64"); err == nil {
		t.Fatal("a plain http feed was accepted")
	}
}
