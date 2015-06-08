package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/pkg/version"
)

type Info struct {
	ClusterURL  string
	InstanceURL string
	Name        string
}

type Instance struct {
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

func RegisterInstance(info Info) (string, error) {
	data := struct {
		Data Instance `json:"data"`
	}{Instance{
		Name:          info.Name,
		URL:           info.InstanceURL,
		SSHPublicKeys: make([]SSHPublicKey, 0, 4),
		FlynnVersion:  version.String(),
	}}

	for _, t := range []string{"dsa", "rsa", "ecdsa", "ed25519"} {
		keyData, err := ioutil.ReadFile(fmt.Sprintf("/etc/ssh/ssh_host_%s_key.pub", t))
		if err != nil {
			// TODO(titanous): log this?
			continue
		}
		k, _, _, _, err := ssh.ParseAuthorizedKey(keyData)
		if err != nil {
			// TODO(titanous): log this?
			continue
		}
		data.Data.SSHPublicKeys = append(data.Data.SSHPublicKeys, SSHPublicKey{Type: t, Data: k.Marshal()})
	}

	jsonData, err := json.Marshal(&data)
	if err != nil {
		return "", err
	}
	// TODO(titanous): retry
	uri := info.ClusterURL + "/instances"
	res, err := http.Post(uri, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	if res.StatusCode != 201 {
		return "", &url.Error{
			Op:  "POST",
			URL: uri,
			Err: fmt.Errorf("unexpected status %d", res.StatusCode),
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.Data.ID, nil
}

func GetCluster(uri string) ([]*Instance, error) {
	res, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, urlError(uri, res.StatusCode)
	}
	defer res.Body.Close()

	var data struct {
		Data []*Instance `json:"data"`
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	return data.Data, err
}

func NewToken() (string, error) {
	const uri = "https://discovery.flynn.io/clusters"
	req, err := http.NewRequest("POST", uri, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("flynn-host/%s %s-%s", version.String(), runtime.GOOS, runtime.GOARCH))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusCreated {
		return "", urlError(uri, res.StatusCode)
	}
	return res.Header.Get("Location"), nil
}

func urlError(uri string, status int) error {
	return &url.Error{
		Op:  "GET",
		URL: uri,
		Err: fmt.Errorf("unexpected status %d", status),
	}

}
