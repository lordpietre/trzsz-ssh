package tssh

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const kFrpVersion = "0.69.1"

func frpDownloadIfNeeded() error {
	if frpFindBin("frps") != "" && frpFindBin("frpc") != "" {
		return nil
	}

	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".tssh", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("cannot create bin directory: %v", err)
	}

	osSuffix := frpPlatformOS()
	archSuffix := frpPlatformArch()
	isWindows := osSuffix == "windows"
	ext := "tar.gz"
	if isWindows {
		ext = "zip"
	}

	url := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/v%s/frp_%s_%s_%s.%s",
		kFrpVersion, kFrpVersion, osSuffix, archSuffix, ext)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if strings.Contains(url, "arm_hf") {
			altURL := strings.Replace(url, "arm_hf", "arm", 1)
			client2 := &http.Client{Timeout: 60 * time.Second}
			resp2, err2 := client2.Get(altURL)
			if err2 != nil {
				return fmt.Errorf("download failed (tried arm_hf and arm): %v", err2)
			}
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				resp = resp2
			} else {
				return fmt.Errorf("download failed: HTTP %d (tried arm_hf: %d)", resp2.StatusCode, resp.StatusCode)
			}
		} else {
			return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
	}

	if isWindows {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response failed: %v", err)
		}
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("zip parse failed: %v", err)
		}
		var found int
		for _, f := range zipReader.File {
			base := filepath.Base(f.Name)
			if base == "frps.exe" || base == "frpc.exe" {
				rc, err := f.Open()
				if err != nil {
					return fmt.Errorf("open zip entry %s: %v", base, err)
				}
				out, err := os.Create(filepath.Join(binDir, base))
				if err != nil {
					rc.Close()
					return fmt.Errorf("create %s: %v", base, err)
				}
				_, err = io.Copy(out, rc)
				rc.Close()
				out.Close()
				if err != nil {
					return fmt.Errorf("extract %s: %v", base, err)
				}
				_ = os.Chmod(filepath.Join(binDir, base), 0755)
				found++
			}
		}
		if found < 2 {
			return fmt.Errorf("extracted %d/2 frp binaries from zip", found)
		}
	} else {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("gzip read failed: %v", err)
		}
		defer gzReader.Close()

		tarReader := tar.NewReader(gzReader)
		var found int
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("tar read failed: %v", err)
			}
			base := filepath.Base(header.Name)
			if base == "frps" || base == "frpc" {
				out, err := os.Create(filepath.Join(binDir, base))
				if err != nil {
					return fmt.Errorf("create %s: %v", base, err)
				}
				_, err = io.Copy(out, tarReader)
				out.Close()
				if err != nil {
					return fmt.Errorf("extract %s: %v", base, err)
				}
				_ = os.Chmod(filepath.Join(binDir, base), 0755)
				found++
			}
		}
		if found < 2 {
			return fmt.Errorf("extracted %d/2 frp binaries from archive", found)
		}
	}

	return nil
}

func frpPlatformOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	case "freebsd":
		return "freebsd"
	case "openbsd":
		return "openbsd"
	default:
		return runtime.GOOS
	}
}

func frpPlatformArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm_hf"
	case "386":
		return "386"
	case "loong64":
		return "loong64"
	case "mips":
		return "mips"
	case "mips64":
		return "mips64"
	case "mips64le":
		return "mips64le"
	case "mipsle":
		return "mipsle"
	case "riscv64":
		return "riscv64"
	default:
		return runtime.GOARCH
	}
}
