package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gonutz/w32"
	"github.com/machinebox/graphql"
	"golang.org/x/sys/windows/registry"
)

type gqlResponse struct {
	DolphinVersions []dolphinVersion `json:"dolphinVersions"`
}

type dolphinVersion struct {
	URL        string `json:"url"`
	Version    string `json:"version"`
	ReleasedAt string `json:"releasedAt"`
	Type       string `json:"type"`
}

func execAppUpdate(isFull, skipUpdaterUpdate, shouldLaunch bool, isoPath, prevVersion string) (returnErr error) {
	defer func() {
		if r := recover(); r != nil {
			returnErr = errors.New("Error encountered updating app")
		}
	}()

	// Get executable path
	ex, err := os.Executable()
	if err != nil {
		log.Panic(err)
	}
	exPath := filepath.Dir(ex)

	oldSlippiToolsPath := filepath.Join(exPath, "old-dolphin-slippi-tools.exe")

	// If we are doing a full update or if we are done updating the updater, wait for Dolphin to close
	if isFull || skipUpdaterUpdate {
		waitForDolphinClose()
	}

	isBeta := strings.Contains(prevVersion, "-beta")
	latest := getLatestVersion(isBeta)
	dir, err := ioutil.TempDir("", "dolphin-update")
	if err != nil {
		log.Panic(err)
	}
	defer os.RemoveAll(dir)

	zipFilePath := filepath.Join(dir, "dolphin.zip")
	err = downloadFile(zipFilePath, latest.URL)
	if err != nil {
		log.Panic(err)
	}

	if !isFull && !skipUpdaterUpdate {
		prevVersionDisplay := prevVersion
		if prevVersionDisplay == "" {
			prevVersionDisplay = "unknown"
		}
		fmt.Printf("Preparing to update app from %s to %s...\n", prevVersionDisplay, latest.Version)

		slippiToolsPath := filepath.Join(exPath, "dolphin-slippi-tools.exe")
		// If we get here, we need to extract the updater. Start by renaming the current updater
		err = os.Rename(slippiToolsPath, oldSlippiToolsPath)
		if err != nil {
			log.Panicf("Failed to rename slippi tools. %s", err.Error())
		}

		// Now extract the updater
		err = extractFiles(exPath, zipFilePath, updaterUpdateGen)
		if err != nil {
			log.Panic(err)
		}

		// Launch the new updater
		launchArg := fmt.Sprintf("-launch=%t", shouldLaunch)
		cmd := exec.Command(slippiToolsPath, "app-update", "-skip-updater", launchArg, "-iso", isoPath, "-version", prevVersion)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stdout
		err = cmd.Start()
		if err != nil {
			log.Panicf("Failed to start app-update with new updater. %s", err.Error())
		}
	} else {
		// Delete old-dolphin-slippi-tools.exe if it exists. Deleting here because we should have waited
		// for Dolphin to close which means the previous updater should no longer be running
		os.RemoveAll(oldSlippiToolsPath)

		// After 2.2.0 we stopped supporting non-melee games by default, this will delete all old inis
		applyMeleeOnlyChanges(prevVersion, exPath)

		// Delete previous install
		err := deletePrevious(exPath)
		if err != nil {
			log.Panicf("Failed to delete old install. %s\n", err.Error())
		}

		// Extract all non-exe files used for update
		err = extractFiles(exPath, zipFilePath, fullUpdateGen)
		if err != nil {
			log.Panic(err)
		}

		// Now extract the exe (do this last such that we can avoid a partial update)
		err = extractFiles(exPath, zipFilePath, exeUpdateGen)
		if err != nil {
			log.Panic(err)
		}

		// Install vcr if the user doesn't already have it installed
		// TODO: Consider not updating vcr if there's a new version
		installVcr(dir)

		if shouldLaunch {
			// Launch Dolphin
			cmd := exec.Command(filepath.Join(exPath, "Slippi Dolphin.exe"), "-e", isoPath)
			cmd.Start()
			if err != nil {
				log.Panicf("Failed to start Dolphin. %s", err.Error())
			}
		}
	}

	return nil
}

func waitForDolphinClose() {
	// TODO: Look for specific dolphin process?

	fmt.Printf("\nYou can find release notes at: https://github.com/project-slippi/Ishiiruka/releases \n\n")
	fmt.Println("Waiting for Dolphin to close. Ensure ALL Dolphin instances are closed. Can take a few moments after they are all closed...")
	for {
		cmd, _ := exec.Command("TASKLIST", "/FI", "IMAGENAME eq Dolphin.exe").Output()
		output := string(cmd[:])
		splitOutp := strings.Split(output, "\n")
		if len(splitOutp) > 3 {
			time.Sleep(500 * time.Millisecond)
			//fmt.Println("Process is running...")
			continue
		}

		cmd, _ = exec.Command("TASKLIST", "/FI", "IMAGENAME eq Slippi Dolphin.exe").Output()
		output = string(cmd[:])
		splitOutp = strings.Split(output, "\n")
		if len(splitOutp) > 3 {
			time.Sleep(500 * time.Millisecond)
			//fmt.Println("Process is running...")
			continue
		}

		// If we get here, process is gone
		break
	}
}

func extractFiles(target, source string, genTargetFile func(string) string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()

	// First find Dolphin.exe
	dolphinPath := ""
	for _, file := range reader.File {
		filePathName := file.Name
		baseFile := filepath.Base(filePathName)

		if baseFile == "Dolphin.exe" || baseFile == "Slippi Dolphin.exe" {
			dolphinPath = filepath.Dir(filePathName)
			break
		}
	}

	// Path pattern
	dolphinPathPattern := filepath.ToSlash(filepath.Join(dolphinPath, "*"))

	// Iterate through all files, deciding whether to extract
	for _, file := range reader.File {
		isMatch, err := filepath.Match(dolphinPathPattern, file.Name)
		if err != nil || !isMatch {
			continue
		}

		relPath, err := filepath.Rel(dolphinPath, file.Name)
		if err != nil {
			continue
		}

		targetRelPath := genTargetFile(relPath)
		if targetRelPath == "" {
			continue
		}

		// Generate target path
		path := filepath.Join(target, targetRelPath)

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		start := time.Now()

		for time.Now().Sub(start) < (time.Second * 20) {
			targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				log.Printf("Failed to open file for write, will try again: %s\n", path)
				time.Sleep(time.Second)
				continue
			}
			defer targetFile.Close()

			if _, err := io.Copy(targetFile, fileReader); err != nil {
				log.Printf("Failed to copy file, will try again: %s\n", path)
				time.Sleep(time.Second)
				continue
			}

			// If everything succeeded, break immediately
			break
		}

		// Return error if there was one above and we timed out
		if err != nil {
			return err
		}

		log.Printf("Finished copying file: %s\n", path)
	}

	return nil
}

func fullUpdateGen(path string) string {
	slashPath := filepath.ToSlash(path)

	// Check if Dolphin.exe
	if slashPath == "Dolphin.exe" || slashPath == "Slippi Dolphin.exe" {
		return ""
	}

	if slashPath == "dolphin-slippi-tools.exe" {
		return ""
	}

	return path
}

func updaterUpdateGen(path string) string {
	if path == "dolphin-slippi-tools.exe" {
		return path
	}

	return ""
}

func exeUpdateGen(path string) string {
	slashPath := filepath.ToSlash(path)

	// Check if Dolphin.exe
	if slashPath == "Dolphin.exe" || slashPath == "Slippi Dolphin.exe" {
		return path
	}

	return ""
}

func deletePrevious(path string) error {
	err := os.RemoveAll(filepath.Join(path, "Dolphin.exe"))
	if err != nil {
		return err
	}

	err = os.RemoveAll(filepath.Join(path, "Slippi Dolphin.exe"))
	if err != nil {
		return err
	}

	err = os.RemoveAll(filepath.Join(path, "Sys"))
	if err != nil {
		return err
	}

	return nil
}

func getLatestVersion(isBeta bool) dolphinVersion {
	// TODO: Cache response?

	client := graphql.NewClient("https://slippi-hasura.herokuapp.com/v1/graphql")
	req := graphql.NewRequest(`
		query ($types: [String!]!) {
			dolphinVersions(order_by: {releasedAt: desc}, where: {type: {_in: $types}}, limit: 1) {
				url
				version
				releasedAt
				type
			}
		}	
	`)

	types := []string{"ishii"}
	if isBeta {
		types = append(types, "ishii-beta")
	}
	req.Var("types", types)
	ctx := context.Background()

	var resp gqlResponse
	err := client.Run(ctx, req, &resp)
	if err != nil {
		log.Printf("Failed to fetch version info from graphql server, got %s", err.Error())
	}

	return resp.DolphinVersions[0]
}

func getCurrentVcrVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\X64`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	major, _, err := k.GetIntegerValue("Major")
	if err != nil {
		return ""
	}

	minor, _, err := k.GetIntegerValue("Minor")
	if err != nil {
		return ""
	}

	bld, _, err := k.GetIntegerValue("Bld")
	if err != nil {
		return ""
	}

	rbld, _, err := k.GetIntegerValue("Rbld")
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%d.%d.%d.%d", major, minor, bld, rbld)
}

func getInstallerVcrVersion(installerPath string) string {
	size := w32.GetFileVersionInfoSize(installerPath)
	if size <= 0 {
		log.Panicf("Couldn't load latest VCR version size")
	}

	info := make([]byte, size)
	ok := w32.GetFileVersionInfo(installerPath, info)
	if !ok {
		log.Panicf("Couldn't load latest VCR version")
	}

	fixed, ok := w32.VerQueryValueRoot(info)
	if !ok {
		log.Panicf("Couldn't load latest VCR version root")
	}

	version := fixed.FileVersion()
	return fmt.Sprintf(
		"%d.%d.%d.%d",
		version&0xFFFF000000000000>>48,
		version&0x0000FFFF00000000>>32,
		version&0x00000000FFFF0000>>16,
		version&0x000000000000FFFF>>0,
	)
}

func installVcr(tempDir string) {
	log.Printf("Checking new VCRuntime installation...")

	vcrFilePath := filepath.Join(tempDir, "vcr.exe")
	err := downloadFile(vcrFilePath, "https://aka.ms/vs/16/release/vc_redist.x64.exe")
	if err != nil {
		log.Panic(err)
	}

	// First let's check if the latest version of VCR is already installed
	currentVersion := getCurrentVcrVersion()
	installerVersion := getInstallerVcrVersion(vcrFilePath)
	log.Printf("Current version: %s, Latest version: %s\n", currentVersion, installerVersion)
	if currentVersion == installerVersion {
		log.Printf("Latest VCR already installed")
		return
	}

	cmd := exec.Command(vcrFilePath, "/install", "/passive", "/norestart")
	err = cmd.Run()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err != nil {
		if err.Error() == "exit status 1638" {
			log.Printf("VCR already installed")
		} else if err.Error() == "exit status 3010" {
			log.Printf("VCR was installed successfully. If you have issues you may need to restart your computer")
		} else {
			log.Panicf("Failed to install VCRuntime. %s", err.Error())
		}
	} else {
		log.Printf("VCR install successful")
	}
}

// DownloadFile will download a url to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
// Taken from: https://golangcode.com/download-a-file-from-a-url/
func downloadFile(filepath string, url string) error {
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func applyMeleeOnlyChanges(prevVersion, exPath string) {
	if prevVersion != "" {
		// Before version 2.2.1, we didn't include previous version, so if this isn't empty,
		// we shouldn't be deleting these files
		return
	}

	gameSettingsPath := filepath.Join(exPath, "Sys", "GameSettings")

	log.Printf("Cleaning up old files...")

	// Attempt to delete all files inside the Sys/GameSettings folder
	dir, err := ioutil.ReadDir(gameSettingsPath)
	for _, d := range dir {
		err = os.RemoveAll(filepath.Join(gameSettingsPath, d.Name()))
		if err != nil {
			log.Panic(err)
		}
	}

	log.Printf("Cleanup complete")
}
