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

	"github.com/machinebox/graphql"
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

	latest := getLatestVersion()
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
		fmt.Println("Preparing to update app...")

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

		// Prepare to extract files
		updateGen := partialUpdateGen
		if isFull {
			updateGen = fullUpdateGen
		}

		err = extractFiles(exPath, zipFilePath, updateGen)
		if err != nil {
			log.Panic(err)
		}

		// Now extract the exe (do this last such that we can avoid a partial update)
		err = extractFiles(exPath, zipFilePath, exeUpdateGen)
		if err != nil {
			log.Panic(err)
		}

		// Install vcr if the user doesn't already have it installed
		installVcr(dir)

		if shouldLaunch {
			// Launch Dolphin
			cmd := exec.Command(filepath.Join(exPath, "Dolphin.exe"), "-e", isoPath)
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
		// TODO: Handle other OS's
		if baseFile == "Dolphin.exe" {
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
	if slashPath == "Dolphin.exe" {
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
	if slashPath == "Dolphin.exe" {
		return path
	}

	return ""
}

func partialUpdateGen(path string) string {
	slashPath := filepath.ToSlash(path)

	// TODO: This really should do something better. This method does not deal with deleted files,
	// TODO: renamed files, different file modifications per-version, etc.

	// Check fix VCRuntime file
	if slashPath == "FIX-VCRUNTIME140-ERROR.txt" {
		return path
	}

	// Check if game file
	gameFilesPattern := "Sys/GameFiles/GALE01/*"
	gameFilesMatch, err := filepath.Match(gameFilesPattern, slashPath)
	if err != nil {
		return ""
	}

	if gameFilesMatch {
		return path
	}

	// Check if code file
	if slashPath == "Sys/GameSettings/GALE01r2.ini" || slashPath == "Sys/GameSettings/GALJ01r2.ini" {
		return path
	}

	// Check if code selection file
	// TODO: Make it so that people's optional selections are preserved
	if slashPath == "User/GameSettings/GALE01.ini" || slashPath == "User/GameSettings/GALJ01r2.ini" {
		return path
	}

	return ""
}

func getLatestVersion() dolphinVersion {
	// TODO: Cache response?

	client := graphql.NewClient("https://slippi-hasura.herokuapp.com/v1/graphql")
	req := graphql.NewRequest(`
		query ($type: String!) {
			dolphinVersions(order_by: {releasedAt: desc}, limit: 1, where: {type: {_eq: $type}}) {
				url
				version
				releasedAt
				type
			}
		}	
	`)

	req.Var("type", "ishii")
	ctx := context.Background()

	var resp gqlResponse
	err := client.Run(ctx, req, &resp)
	if err != nil {
		log.Printf("Failed to fetch version info from graphql server, got %s", err.Error())
	}

	return resp.DolphinVersions[0]
}

func installVcr(tempDir string) {
	log.Printf("Checking new VCRuntime installation...")

	vcrFilePath := filepath.Join(tempDir, "vcr.exe")
	err := downloadFile(vcrFilePath, "https://aka.ms/vs/16/release/vc_redist.x64.exe")
	if err != nil {
		log.Panic(err)
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
