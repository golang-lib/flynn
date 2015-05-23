package etcdcluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	URLs []string
}

func (c *Client) AddMember(url string) error {
	data, err := json.Marshal(map[string][]string{"peerURLs": {url}})
	if err != nil {
		return err
	}
	for _, url := range c.URLs {
		var res *http.Response
		res, err = http.Post(url+"/v2/members", "application/json", bytes.NewReader(data))
		if err != nil {
			continue
		}
		res.Body.Close()
		if res.StatusCode != 201 && res.StatusCode != 409 {
			return fmt.Errorf("etcd: unexpected status %d adding member", res.StatusCode)
		}
		return nil
	}
	return err
}

func (c *Client) GetMembers() ([]Member, error) {
	var err error
	for _, url := range c.URLs {
		var res *http.Response
		res, err = http.Get(url + "/v2/members")
		if err != nil {
			continue
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("etcd: unexpected status %d getting members", res.StatusCode)
		}
		var data struct {
			Members []Member `json:"members"`
		}
		err = json.NewDecoder(res.Body).Decode(&data)
		res.Body.Close()
		return data.Members, err
	}
	return nil, err
}

type Member struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	PeerURLs   []string `json:"peerURLs"`
	ClientURLs []string `json:"clientURLs"`
}
