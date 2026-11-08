package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	return manifestFor(version, "v"+version, file, size, sum)
}

// manifestFor is a manifest naming its release both ways the fork publishes:
// the machine-facing version and the tag it was released under.
func manifestFor(version, tag, file string, size int64, sum string) string {
	return fmt.Sprintf(`{"version":%q,"tag":%q,"source":"https://github.com/owner/repo/releases/tag/%s","notes":"notes","files":[{"name":%q,"size":%d,"sha256":%q,"signature":%q}]}`,
		version, tag, tag, file, size, sum, goodSignature)
}

const setupName = "motty-0.3.1-windows-amd64-setup.exe"

func TestTagForNamesBuildsOnTheUpstreamBase(t *testing.T) {
	for _, test := range []struct {
		version, base, codename, want string
	}{
		{version: "0.3.1.4", base: "0.3.1", codename: "Arknights", want: "v0.3.1-netcli-build.4-arknights"},
		{version: "0.3.1.12", base: "0.3.1", codename: "Arknights", want: "v0.3.1-netcli-build.12-arknights"},
		{version: "0.3.1.4", base: "0.3.1", codename: "arknights", want: "v0.3.1-netcli-build.4-arknights"},
		// A build on a newer upstream line names that line.
		{version: "0.4.2.3", base: "0.4.2", codename: "Arknights", want: "v0.4.2-netcli-build.3-arknights"},
		// Not on the base's line, or no counter yet: a plain tag, so a
		// misconfigured build never borrows the fork's name.
		{version: "0.4.1.1", base: "0.3.1", codename: "Arknights", want: "v0.4.1.1"},
		{version: "1.0.1.1", base: "0.3.1", codename: "Arknights", want: "v1.0.1.1"},
		{version: "0.3.1.0", base: "0.3.1", codename: "Arknights", want: "v0.3.1.0"},
		{version: "0.3.5", base: "0.3.1", codename: "Arknights", want: "v0.3.5"},
	} {
		if got := TagFor(test.version, test.base, test.codename); got != test.want {
			t.Errorf("TagFor(%q, %q, %q) = %q, want %q", test.version, test.base, test.codename, got, test.want)
		}
	}
}

func TestVersionFromTagReadsBothShapes(t *testing.T) {
	for _, test := range []struct {
		tag  string
		want string
		ok   bool
	}{
		{tag: "v0.3.1-netcli-build.4-arknights", want: "0.3.1.4", ok: true},
		{tag: "v0.4.2-netcli-build.12-arknights", want: "0.4.2.12", ok: true},
		// Releases published before this naming keep working.
		{tag: "v0.3.0-netcli-build.2-arknights", want: "0.3.0.2", ok: true},
		{tag: "v0.3.1", want: "0.3.1", ok: true},
		{tag: "v0.3.1.4", want: "0.3.1.4", ok: true},
		// A shape the scheme does not use is not a tag this fork publishes.
		{tag: "v0.3.1-nightly", ok: false},
		{tag: "v0.3.1-netcli-build.9-arknights-evil", ok: false},
		{tag: "v0.3.1-rc1", ok: false},
		{tag: "nightly", ok: false},
	} {
		got, ok := VersionFromTag(test.tag)
		if ok != test.ok || (ok && got != test.want) {
			t.Errorf("VersionFromTag(%q) = %q, %v; want %q, %v", test.tag, got, ok, test.want, test.ok)
		}
	}
}

// A mirror is not the publisher: whatever page and notes its manifest
// carries, the answer it gives must point at the repository's own release
// page and carry no markdown of the mirror's choosing.
func TestAMirrorAnswerNeverCarriesTheMirrorsPageOrNotes(t *testing.T) {
	const tag = "v0.3.1-netcli-build.2-arknights"
	evil := fmt.Sprintf(`{"version":"0.3.1.2","tag":%q,"source":"https://evil.example/phish","notes":"[click here](https://evil.example)","files":[{"name":"motty-0.3.1.2-windows-amd64-setup.exe","size":42,"sha256":"%s","signature":%q}]}`,
		tag, goodHash, goodSignature)
	server := forkServer(t, nil, evil)
	release, err := mirrorRelease(context.Background(), server.Client(), server.URL+"/manifest.json", forkRepo, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://github.com/" + forkRepo + "/releases/tag/" + tag
	if release.URL != want {
		t.Fatalf("page = %q, want the pinned %q", release.URL, want)
	}
	if release.Notes != "" {
		t.Fatalf("notes = %q, want none from a mirror", release.Notes)
	}
	if !release.Asset.Has() || !strings.HasPrefix(release.Asset.URL, "https://github.com/") {
		t.Fatalf("asset = %+v", release.Asset)
	}
}

func TestLatestForkReadsATaggedBuild(t *testing.T) {
	const tag = "v0.3.1-netcli-build.2-arknights"
	server := forkServer(t, releaseWithAssets(tag),
		manifestFor("0.3.1.2", tag, "motty-0.3.1.2-windows-amd64-setup.exe", 42, goodHash))
	release, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "0.3.1.2" || !release.PlatformPackageChecked {
		t.Fatalf("release %+v", release)
	}
	// The download comes from the tagged directory, under the machine-facing
	// name the installer itself carries.
	want := "https://github.com/owner/repo/releases/download/" + tag + "/motty-0.3.1.2-windows-amd64-setup.exe"
	if release.Asset.URL != want || release.Asset.Name != "motty-0.3.1.2-windows-amd64-setup.exe" {
		t.Fatalf("asset %+v", release.Asset)
	}
}

func TestLatestForkRefusesAManifestForAnotherTag(t *testing.T) {
	// A manifest whose tag does not match the release it came with is a stale
	// copy, whichever naming it was written for.
	server := forkServer(t, releaseWithAssets("v0.3.1-netcli-build.2-arknights"),
		`{"version":"0.3.1.3","tag":"v0.3.1","source":"https://github.com/owner/repo/releases/tag/v0.3.1","files":[]}`)
	if _, err := LatestFork(context.Background(), server.Client(), server.URL+"/latest", forkRepo, "windows", "amd64"); err == nil {
		t.Fatal("a manifest for another tag was accepted")
	}
}

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

// checkServers is one TLS server standing in for every host the check reads:
// the GitHub feed at /latest, GitHub's manifest at /manifest, and the CDN
// mirrors at /mirror. Which of them answer is the test's choice.
type checkServers struct {
	feedStatus int
	feedBody   string
	manifestOn bool
	mirrorOn   bool
	mirrorBad  bool // the mirror serves a manifest that contradicts itself
}

func (s *checkServers) start(t *testing.T) (*http.Client, func()) {
	t.Helper()
	savedFeed, savedMirrors := forkFeedURL, feedMirrors
	var manifestURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			if s.feedStatus != http.StatusOK {
				w.WriteHeader(s.feedStatus)
				return
			}
			fmt.Fprintf(w, `{"tag_name":"v0.3.1-netcli-build.3-arknights","html_url":"https://github.com/owner/repo/releases/tag/v0.3.1-netcli-build.3-arknights","assets":[{"name":"latest.json","browser_download_url":%q}]}`, manifestURL)
		case "/manifest":
			if !s.manifestOn {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, manifestFor("0.3.1.3", "v0.3.1-netcli-build.3-arknights", "motty-0.3.1.3-windows-amd64-setup.exe", 42, goodHash))
		case "/mirror":
			if !s.mirrorOn {
				http.NotFound(w, r)
				return
			}
			if s.mirrorBad {
				fmt.Fprint(w, manifestFor("0.3.1.3", "v0.3.1-netcli-build.2-arknights", "motty-0.3.1.3-windows-amd64-setup.exe", 42, goodHash))
				return
			}
			fmt.Fprint(w, manifestFor("0.3.1.3", "v0.3.1-netcli-build.3-arknights", "motty-0.3.1.3-windows-amd64-setup.exe", 42, goodHash))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	manifestURL = server.URL + "/manifest"
	forkFeedURL = server.URL + "/latest"
	feedMirrors = []string{server.URL + "/mirror"}
	return server.Client(), func() { forkFeedURL, feedMirrors = savedFeed, savedMirrors }
}

func TestCheckPrefersGitHubAndFallsBackInOrder(t *testing.T) {
	for _, test := range []struct {
		name     string
		servers  checkServers
		want     string // the version the check concludes, "" for an error
		wantPage bool   // a release with its page and no package
	}{
		{
			name:    "github in full",
			servers: checkServers{feedStatus: 200, manifestOn: true},
			want:    "0.3.1.3",
		},
		{
			name:    "manifest unreachable, a mirror has it",
			servers: checkServers{feedStatus: 200, mirrorOn: true},
			want:    "0.3.1.3",
		},
		{
			name:     "manifest nowhere, the release page still answers",
			servers:  checkServers{feedStatus: 200},
			want:     "0.3.1.3",
			wantPage: true,
		},
		{
			name:    "feed down, a mirror has the manifest",
			servers: checkServers{feedStatus: http.StatusInternalServerError, mirrorOn: true},
			want:    "0.3.1.3",
		},
		{
			name:    "nothing anywhere",
			servers: checkServers{feedStatus: http.StatusInternalServerError},
			want:    "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, restore := test.servers.start(t)
			defer restore()
			release, err := Check(context.Background(), client, "windows", "amd64")
			if test.want == "" {
				if err == nil {
					t.Fatalf("an all-around failure answered %+v", release)
				}
				return
			}
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if release.Version != test.want {
				t.Fatalf("version %q, want %q", release.Version, test.want)
			}
			if release.Asset.Has() == test.wantPage {
				t.Fatalf("asset %+v, wantPage %v", release.Asset, test.wantPage)
			}
		})
	}
}

func TestCheckRejectsAMirrorServingAnotherRelease(t *testing.T) {
	// A manifest whose tag and version contradict each other is not a release
	// this fork publishes; with nothing else answering, the check fails
	// instead of passing the manifest off as news.
	servers := checkServers{feedStatus: http.StatusInternalServerError, mirrorOn: true, mirrorBad: true}
	client, restore := servers.start(t)
	defer restore()
	if _, err := Check(context.Background(), client, "windows", "amd64"); err == nil {
		t.Fatal("an incoherent mirror manifest was accepted")
	}
}
