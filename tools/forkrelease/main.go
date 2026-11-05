// Command forkrelease prepares this fork's release assets: it makes the
// signing key once, then writes the latest.json manifest a build checks for
// updates, signed so a download can be held to the release key.
//
//	forkrelease keygen   -private <path> [-public <path>]
//	forkrelease manifest -version 0.3.1.4 -pkg <setup.exe> -key <private key> [-out latest.json]
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"aead.dev/minisign"

	"motty/internal/update"
	"motty/internal/updatesign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "manifest":
		err = manifest(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "forkrelease:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  forkrelease keygen   -private <path> [-public <path>]
  forkrelease manifest -version <version> -pkg <file> -key <private key> [-out latest.json]
`)
}

// keygen makes the key pair the fork signs its releases with. The private half
// must stay off this repository: it is what makes an update trustworthy.
func keygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	privatePath := flags.String("private", "", "where to write the private key")
	publicPath := flags.String("public", "", "where to write the public key (optional; it is also printed)")
	password := flags.String("password", "", "encrypt the private key with this password (optional)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *privatePath == "" {
		return errors.New("keygen needs -private")
	}
	if _, err := os.Stat(*privatePath); err == nil {
		return fmt.Errorf("%s already exists; refusing to replace a signing key", *privatePath)
	}
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	publicText, err := publicKey.MarshalText()
	if err != nil {
		return err
	}
	privateText, err := privateKey.MarshalText()
	if *password != "" {
		privateText, err = minisign.EncryptKey(*password, privateKey)
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(*privatePath, privateText, 0o600); err != nil {
		return err
	}
	if *publicPath != "" {
		if err := os.WriteFile(*publicPath, publicText, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "private key: %s (keep it off the repository and back it up)\n", *privatePath)
	fmt.Print(string(publicText))
	return nil
}

// manifest describes and signs one package for the update feed. The signature
// covers the package's bytes, the version and the file name, so a feed cannot
// pass one build off as another. The version is the machine-facing one — the
// upstream base plus a build counter, 0.3.1.4, 0.3.1.5 — and the published tag
// names the upstream base and the era's codename on top of it:
// v0.3.1-netcli-build.4-arknights.
func manifest(args []string) error {
	flags := flag.NewFlagSet("manifest", flag.ExitOnError)
	version := flags.String("version", "", "machine-facing version without the leading v, e.g. 0.3.1.4")
	pkg := flags.String("pkg", "", "the package to sign")
	keyPath := flags.String("key", "", "the minisign private key")
	password := flags.String("password", "", "password of an encrypted private key, or $MOTTY_FORK_KEY_PASSWORD")
	base := flags.String("base", update.ForkBase, "the upstream version this build sits on")
	codename := flags.String("codename", update.ForkCodename, "this era of the fork")
	repo := flags.String("repo", update.ForkRepo, "owner/repository the release is published to")
	notesPath := flags.String("notes", "", "file holding the release notes")
	out := flags.String("out", "latest.json", "where to write the manifest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *pkg == "" || *keyPath == "" {
		return errors.New("manifest needs -version, -pkg and -key")
	}
	tag := update.TagFor(*version, *base, *codename)
	if tag == "v"+*version {
		return fmt.Errorf("version %s is not a build on top of %s; the tag would carry no build number", *version, *base)
	}
	name := filepath.Base(*pkg)
	size, sum, err := fileDigest(*pkg)
	if err != nil {
		return err
	}
	privateKey, err := loadPrivateKey(*keyPath, *password)
	if err != nil {
		return fmt.Errorf("reading the signing key: %w", err)
	}
	packageFile, err := os.Open(*pkg)
	if err != nil {
		return err
	}
	defer packageFile.Close()
	signature, err := updatesign.Sign(privateKey, packageFile, *version, name, time.Now())
	if err != nil {
		return err
	}
	// A package signed with a key this build does not trust installs nowhere.
	// Checking here turns a wrong -key into an error before anything is published.
	if err := updatesign.VerifyFile(updatesign.ReleaseKey(), *pkg, signature, *version, name); err != nil {
		return fmt.Errorf("this key is not the one embedded in the build: %w", err)
	}
	notes := ""
	if *notesPath != "" {
		data, err := os.ReadFile(*notesPath)
		if err != nil {
			return err
		}
		notes = string(data)
	}
	feed := manifestFile{
		Version: *version,
		Tag:     tag,
		Source:  "https://github.com/" + *repo + "/releases/tag/" + tag,
		Notes:   notes,
		Files: []manifestEntry{{
			Name: name, Size: size, SHA256: sum, Signature: signature,
		}},
	}
	data, err := json.MarshalIndent(feed, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %s (%d bytes)\n", name, sum, size)
	return nil
}

// loadPrivateKey reads the key whether it was written plain or encrypted. The
// minisign library's own file loader only takes encrypted keys, so this picks
// the path by what the file says.
func loadPrivateKey(path, password string) (minisign.PrivateKey, error) {
	if password == "" {
		password = os.Getenv("MOTTY_FORK_KEY_PASSWORD")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return minisign.PrivateKey{}, err
	}
	if minisign.IsEncrypted(data) {
		if password == "" {
			return minisign.PrivateKey{}, errors.New("the key is encrypted; pass -password or MOTTY_FORK_KEY_PASSWORD")
		}
		return minisign.DecryptKey(password, data)
	}
	var key minisign.PrivateKey
	if err := key.UnmarshalText(data); err != nil {
		return minisign.PrivateKey{}, err
	}
	return key, nil
}

// manifestFile is what the app reads: the same shape the upstream mirror
// serves, which is what internal/update parses.
type manifestFile struct {
	Version string          `json:"version"`
	Tag     string          `json:"tag"`
	Source  string          `json:"source"`
	Notes   string          `json:"notes"`
	Files   []manifestEntry `json:"files"`
}

type manifestEntry struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

func fileDigest(path string) (size int64, sum string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err = io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
