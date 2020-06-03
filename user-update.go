package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/machinebox/graphql"
)

type userGqlResponse struct {
	User            userFile         `json:"user"`
	DolphinVersions []dolphinVersion `json:"dolphinVersions"`
}

type userFile struct {
	UID           string `json:"uid"`
	PlayKey       string `json:"playKey"`
	ConnectCode   string `json:"connectCode"`
	DisplayName   string `json:"displayName"`
	LatestVersion string `json:"latestVersion"`
}

func execUserUpdate() {
	// Get executable path
	ex, err := os.Executable()
	if err != nil {
		log.Panic(err)
	}
	exPath := filepath.Dir(ex)

	file := parseCurrentFile(exPath)
	resp := getGqlResponse(file.UID)

	file.ConnectCode = resp.User.ConnectCode
	file.LatestVersion = resp.DolphinVersions[0].Version

	contents, err := json.Marshal(file)
	if err != nil {
		log.Panicf("Failed to create json file, got %s", err.Error())
	}

	err = ioutil.WriteFile(filepath.Join(exPath, "user.json"), contents, 0644)
	if err != nil {
		log.Panicf("Failed to write user json file, got %s", err.Error())
	}
}

func parseCurrentFile(exPath string) userFile {
	f, err := os.Open(filepath.Join(exPath, "user.json"))
	if err != nil {
		log.Panicf("Could not open user.json file, got %s", err.Error())
	}

	decoder := json.NewDecoder(f)

	var uf userFile
	err = decoder.Decode(&uf)
	if err != nil {
		log.Panicf("Failed to get message type, got %s", err.Error())
	}

	return uf
}

func getGqlResponse(uid string) userGqlResponse {
	client := graphql.NewClient("https://slippi-hasura.herokuapp.com/v1/graphql")
	req := graphql.NewRequest(`
		query ($type: String!, $uid: String!) {
			dolphinVersions(order_by: {releasedAt: desc}, limit: 1, where: {type: {_eq: $type}}) {
				version
			}
			user (uid: $uid) {
				uid
				connectCode
			}
		}	
	`)

	req.Var("type", "ishii")
	req.Var("uid", uid)
	ctx := context.Background()

	var resp userGqlResponse
	err := client.Run(ctx, req, &resp)
	if err != nil {
		log.Panicf("Failed to fetch user info from graphql server, got %s", err.Error())
	}

	return resp
}
