package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/flynn/flynn/pkg/version"
	"golang.org/x/crypto/ssh"
)

type DiscoveryInfo struct {
	ClusterURL  string
	InstanceURL string
	Name        string
}

type DiscoveryInstance struct {
	ID            string         `json:"id,omitempty"`
	ClusterID     string         `json:"cluster_id,omitempty"`
	FlynnVersion  string         `json:"flynn_version,omitempty"`
	SSHPublicKeys []SSHPublicKey `json:"ssh_public_keys,omitempty"`
	URL           string         `json:"url,omitempty"`
	Name          string         `json:"name,omitempty"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
}

type SSHPublicKey struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

func RegisterWithDiscovery(info DiscoveryInfo) (string, error) {
	data := struct {
		Data DiscoveryInstance `json:"data"`
	}{{
		Name:          info.Name,
		URL:           info.InstanceURL,
		SSHPublicKeys: make([]SSHPublicKey, 0, 4),
		FlynnVersion:  version.String(),
	}}

	for _, t := range []string{"dsa", "rsa", "ecdsa", "ed25519"} {
		keyData, err := ioutil.ReadFile(fmt.Sprintf("/etc/ssh/ssh_host_%s_key.pub", t))
		if err != nil {
			// log/skip
		}
		k, _, _, _, err := ssh.ParseAuthorizedKey(keyData)
		if err != nil {
			// log/skip
		}
		data.Data.SSHPublicKeys = append(data.Data.SSHPublicKeys, SSHPublicKey{Type: t, Data: k.Marshal()})
	}

	jsonData, err := json.Marshal(&data)
	if err != nil {
		return "", err
	}
	// TODO: retry
	res, err := http.Post(info.ClusterURL+"/instances", "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	if res.StatusCode != 201 {
		// error
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.Data.ID, nil
}
