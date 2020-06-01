package main

import (
	"archive/zip"
	"context"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"

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

func execAppUpdate(isFull bool) {
	latest := getLatestVersion()
	dir, err := ioutil.TempDir("", "dolphin-update")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	zipFilePath := filepath.Join(dir, "dolphin.zip")
	err = downloadFile(zipFilePath, latest.URL)
	if err != nil {
		log.Fatal(err)
	}

	// Get executable path
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)

	err = extractFiles(exPath, zipFilePath, isFull)
	if err != nil {
		log.Fatal(err)
	}
}

func extractFiles(target, source string, isFull bool) error {
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

		// Check if this file should be extracted
		shouldExtract := shouldExtractFile(relPath, isFull)
		if !shouldExtract {
			continue
		}

		// Generate target path
		path := filepath.Join(target, relPath)

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}
	}

	return nil
}

func shouldExtractFile(path string, isFull bool) bool {
	if isFull {
		return true
	}

	path = filepath.ToSlash(path)

	// TODO: This really should do something better. This method does not deal with deleted files,
	// TODO: renamed files, different file modifications per-version, etc.

	// Check if Dolphin.exe
	if path == "Dolphin.exe" {
		return true
	}

	// Check if game file
	gameFilesPattern := "Sys/GameFiles/GALE01/*"
	gameFilesMatch, err := filepath.Match(gameFilesPattern, path)
	if err != nil {
		return false
	}

	if gameFilesMatch {
		return true
	}

	// Check if code file
	if path == "Sys/GameSettings/GALE01r2.ini" || path == "Sys/GameSettings/GALJ01r2.ini" {
		return true
	}

	return false
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
