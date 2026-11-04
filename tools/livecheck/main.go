// Command livecheck runs the real update check against the live feeds and
// prints what an installed build would conclude: which layer answered, how
// long it took, and whether a package came with it. Use it to tell a broken
// feed from a broken app — its output is safe to share.
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"motty/internal/update"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	release, err := update.Check(ctx, nil, runtime.GOOS, runtime.GOARCH)
	fmt.Println("took:", time.Since(started).Round(time.Millisecond))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("version:", release.Version)
	fmt.Println("page:", release.URL)
	if release.Asset.Has() {
		fmt.Println("asset:", release.Asset.Name, release.Asset.Size, "bytes")
		fmt.Println("asset url:", release.Asset.URL)
		fmt.Println("signed:", release.Asset.Signed())
	} else {
		fmt.Println("asset: none (download page only)")
	}
	fmt.Println("newer than", update.Current, ":", update.Newer(update.Current, release.Version))
}
