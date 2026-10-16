// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (s *Server) originalUpload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 18<<20)
	if e := r.ParseMultipartForm(18 << 20); e != nil {
		return bad("InvalidRequest")
	}
	defer r.MultipartForm.RemoveAll()
	file, header, e := r.FormFile("file")
	if e != nil {
		return bad("InvalidRequest")
	}
	defer file.Close()
	request := r.Clone(r.Context())
	query := r.URL.Query()
	query.Set("key", query.Get("prefix")+header.Filename)
	request.URL.RawQuery = query.Encode()
	request.Body = io.NopCloser(file)
	request.Header.Set("Content-Type", header.Header.Get("Content-Type"))
	return s.putObject(w, request)
}
func (s *Server) expandedKeys(r *http.Request, keys []string) ([]string, error) {
	c, e := s.client(r)
	if e != nil {
		return nil, e
	}
	out := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" {
			return nil, bad("InvalidObjectKey")
		}
		if !strings.HasSuffix(key, "/") {
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
			continue
		}
		pager := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: bucket(r), Prefix: &key})
		for pager.HasMorePages() {
			p, e := pager.NextPage(r.Context())
			if e != nil {
				return nil, e
			}
			for _, o := range p.Contents {
				k := aws.ToString(o.Key)
				if !seen[k] {
					seen[k] = true
					out = append(out, k)
				}
			}
		}
	}
	return out, nil
}
func (s *Server) originalDelete(w http.ResponseWriter, r *http.Request) error {
	var keys []string
	if e := decode(w, r, &keys); e != nil {
		return e
	}
	expanded, e := s.expandedKeys(r, keys)
	if e != nil {
		return e
	}
	for _, key := range expanded {
		clone := r.Clone(r.Context())
		clone.URL.RawQuery = url.Values{"key": {key}}.Encode()
		if e = s.deleteObject(newResponseBuffer(), clone); e != nil {
			return e
		}
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) originalMove(w http.ResponseWriter, r *http.Request) error {
	var v struct {
		Keys         []string `json:"keys"`
		TargetBucket string   `json:"targetBucket"`
		TargetPrefix string   `json:"targetPrefix"`
		Copy         bool     `json:"copy"`
	}
	if e := decode(w, r, &v); e != nil {
		return e
	}
	if v.TargetBucket == "" {
		return bad("InvalidBucketName")
	}
	expanded, e := s.expandedKeys(r, v.Keys)
	if e != nil {
		return e
	}
	for _, key := range expanded {
		relative := key[strings.LastIndex(key, "/")+1:]
		for _, root := range v.Keys {
			if strings.HasSuffix(root, "/") && strings.HasPrefix(key, root) {
				trim := strings.TrimSuffix(root, "/")
				relative = trim[strings.LastIndex(trim, "/")+1:] + "/" + strings.TrimPrefix(key, root)
				break
			}
		}
		destination := v.TargetPrefix
		if destination != "" && !strings.HasSuffix(destination, "/") {
			destination += "/"
		}
		destination += relative
		body, _ := json.Marshal(map[string]any{"key": key, "destination": destination, "destinationBucket": v.TargetBucket, "move": !v.Copy})
		clone := r.Clone(r.Context())
		clone.Body = io.NopCloser(bytes.NewReader(body))
		if e = s.copyObject(newResponseBuffer(), clone); e != nil {
			return e
		}
	}
	w.WriteHeader(204)
	return nil
}
