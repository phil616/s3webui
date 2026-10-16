// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"
)

func (s *Server) client(r *http.Request) (*s3.Client, error) {
	u := user(r)
	selected := u.CredentialID
	// Multipart requests pin their connection so a different tab cannot redirect parts.
	if raw := r.Header.Get("X-S3-Credential-ID"); raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			return nil, bad("InvalidRequest")
		}
		selected = &id
	}
	if selected == nil {
		return nil, &apiError{409, "SelectCredential"}
	}
	var c credential
	var encrypted, token []byte
	e := s.db.QueryRow(r.Context(), `SELECT c.endpoint,c.region,c.access_key,c.secret,c.path_style,c.session_token FROM ui_credentials c WHERE c.id=$3 AND c.enabled AND `+allowedSQL, u.Email, u.Role, *selected).Scan(&c.Endpoint, &c.Region, &c.AccessKey, &encrypted, &c.PathStyle, &token)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, &apiError{403, "AccessDenied"}
	}
	if e != nil {
		return nil, e
	}
	secret, e := unseal(s.cipher, encrypted)
	if e != nil {
		return nil, e
	}
	sessionToken := ""
	if len(token) > 0 {
		sessionToken, e = unseal(s.cipher, token)
		if e != nil {
			return nil, e
		}
	}
	return s3.New(s3.Options{Region: c.Region, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKey, secret, sessionToken), BaseEndpoint: ptr(c.Endpoint), UsePathStyle: c.PathStyle, RetryMaxAttempts: 3, HTTPClient: s.httpClient}), nil
}
func bucket(r *http.Request) *string { return aws.String(r.PathValue("bucket")) }
func key(r *http.Request) (*string, error) {
	k := r.URL.Query().Get("key")
	if k == "" || len([]byte(k)) > 1024 {
		return nil, bad("InvalidObjectKey")
	}
	return aws.String(k), nil
}
func pageSize(r *http.Request) (int32, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 100, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 || n > 1000 {
		return 0, bad("InvalidPageSize")
	}
	return int32(n), nil
}
func (s *Server) buckets(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	n, e := pageSize(r)
	if e != nil {
		return e
	}
	o, e := c.ListBuckets(r.Context(), &s3.ListBucketsInput{MaxBuckets: aws.Int32(n), ContinuationToken: ptr(r.URL.Query().Get("token"))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) createBucket(w http.ResponseWriter, r *http.Request) (err error) {
	var v struct {
		Name   string `json:"name"`
		Region string `json:"region"`
	}
	if e := decode(w, r, &v); e != nil {
		return e
	}
	if v.Name == "" {
		return bad("InvalidBucketName")
	}
	c, e := s.client(r)
	if e != nil {
		return e
	}
	region := v.Region
	if region == "" {
		region = c.Options().Region
	}
	in := &s3.CreateBucketInput{Bucket: aws.String(v.Name)}
	if region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(region)}
	}
	defer func() { s.record(r, "CREATE_BUCKET", v.Name, "", err) }()
	o, e := c.CreateBucket(r.Context(), in)
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) deleteBucket(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	defer func() { s.record(r, "DELETE_BUCKET", r.PathValue("bucket"), "", err) }()
	_, err = c.DeleteBucket(r.Context(), &s3.DeleteBucketInput{Bucket: bucket(r)})
	if err == nil {
		w.WriteHeader(204)
	}
	return err
}
func (s *Server) objects(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	n, e := pageSize(r)
	if e != nil {
		return e
	}
	q := r.URL.Query()
	delimiter := "/"
	if q.Get("recursive") == "true" {
		delimiter = ""
	}
	o, e := c.ListObjectsV2(r.Context(), &s3.ListObjectsV2Input{Bucket: bucket(r), Prefix: ptr(q.Get("prefix")), Delimiter: ptr(delimiter), ContinuationToken: ptr(q.Get("token")), MaxKeys: aws.Int32(n)})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) versions(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	n, e := pageSize(r)
	if e != nil {
		return e
	}
	q := r.URL.Query()
	o, e := c.ListObjectVersions(r.Context(), &s3.ListObjectVersionsInput{Bucket: bucket(r), Prefix: ptr(q.Get("prefix")), KeyMarker: ptr(q.Get("keyMarker")), VersionIdMarker: ptr(q.Get("versionMarker")), MaxKeys: aws.Int32(n)})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) head(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, e := key(r)
	if e != nil {
		return e
	}
	o, e := c.HeadObject(r.Context(), &s3.HeadObjectInput{Bucket: bucket(r), Key: k, VersionId: ptr(r.URL.Query().Get("versionId"))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, e := key(r)
	if e != nil {
		return e
	}
	var streamErr error
	defer func() {
		if streamErr != nil {
			s.record(r, "DOWNLOAD", r.PathValue("bucket"), *k, streamErr)
		} else {
			s.record(r, "DOWNLOAD", r.PathValue("bucket"), *k, err)
		}
	}()
	o, e := c.GetObject(r.Context(), &s3.GetObjectInput{Bucket: bucket(r), Key: k, VersionId: ptr(r.URL.Query().Get("versionId")), Range: ptr(r.Header.Get("Range")), IfMatch: ptr(r.Header.Get("If-Match")), IfNoneMatch: ptr(r.Header.Get("If-None-Match"))})
	if e != nil {
		var re *awshttp.ResponseError
		if errors.As(e, &re) && re.HTTPStatusCode() == 304 {
			w.WriteHeader(304)
			return nil
		}
		return e
	}
	defer o.Body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": *k}))
	w.Header().Set("Accept-Ranges", "bytes")
	if o.ContentLength != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(*o.ContentLength, 10))
	}
	if o.ETag != nil {
		w.Header().Set("ETag", *o.ETag)
	}
	if o.VersionId != nil {
		w.Header().Set("x-amz-version-id", *o.VersionId)
	}
	if o.LastModified != nil {
		w.Header().Set("Last-Modified", o.LastModified.UTC().Format(http.TimeFormat))
	}
	if o.ContentRange != nil {
		w.Header().Set("Content-Range", *o.ContentRange)
		w.WriteHeader(206)
	}
	_, streamErr = io.Copy(w, o.Body)
	// Headers are already sent; audit a failed stream without writing a JSON error into the file.
	return nil
}

// Bounded single PUT for empty folder markers and small uploads; large objects use multipart.
const maxSinglePut = 16 << 20

func (s *Server) putObject(w http.ResponseWriter, r *http.Request) (err error) {
	select {
	case s.transferSlots <- struct{}{}:
		defer func() { <-s.transferSlots }()
	default:
		return &apiError{429, "TransferBusy"}
	}
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, e := key(r)
	if e != nil {
		return e
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSinglePut))
	if e != nil {
		return &apiError{413, "UseMultipart"}
	}
	defer func() { s.record(r, "PUT_OBJECT", r.PathValue("bucket"), *k, err) }()
	o, e := c.PutObject(r.Context(), &s3.PutObjectInput{Bucket: bucket(r), Key: k, Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))), ContentType: ptr(r.Header.Get("Content-Type"))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) deleteObject(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, e := key(r)
	if e != nil {
		return e
	}
	defer func() { s.record(r, "DELETE_OBJECT", r.PathValue("bucket"), *k, err) }()
	o, e := c.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: bucket(r), Key: k, VersionId: ptr(r.URL.Query().Get("versionId"))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func copySource(b, k, v string) string {
	out := strings.ReplaceAll(url.PathEscape(b+"/"+k), "+", "%2B")
	if v != "" {
		out += "?versionId=" + url.QueryEscape(v)
	}
	return out
}
func (s *Server) copyObject(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	var v struct {
		Key               string `json:"key"`
		Destination       string `json:"destination"`
		DestinationBucket string `json:"destinationBucket"`
		VersionID         string `json:"versionId"`
		Move              bool   `json:"move"`
	}
	if e = decode(w, r, &v); e != nil {
		return e
	}
	if v.DestinationBucket == "" {
		v.DestinationBucket = r.PathValue("bucket")
	}
	if v.Key == "" || v.Destination == "" || len([]byte(v.Key)) > 1024 || len([]byte(v.Destination)) > 1024 || (v.Key == v.Destination && v.DestinationBucket == r.PathValue("bucket")) {
		return bad("InvalidObjectKey")
	}
	defer func() { s.record(r, "COPY_OBJECT", r.PathValue("bucket"), v.Key, err) }()
	h, e := c.HeadObject(r.Context(), &s3.HeadObjectInput{Bucket: bucket(r), Key: &v.Key, VersionId: ptr(v.VersionID)})
	if e != nil {
		return e
	}
	if aws.ToInt64(h.ContentLength) > 5*1024*1024*1024 {
		return &apiError{422, "MultipartCopyRequired"}
	}
	o, e := c.CopyObject(r.Context(), &s3.CopyObjectInput{Bucket: &v.DestinationBucket, Key: &v.Destination, CopySource: aws.String(copySource(r.PathValue("bucket"), v.Key, v.VersionID)), CopySourceIfMatch: h.ETag})
	if e != nil {
		return e
	}
	// Copy + delete is deliberately not an atomic rename. Report partial success explicitly.
	if v.Move {
		_, e = c.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: bucket(r), Key: &v.Key, VersionId: ptr(v.VersionID), IfMatch: h.ETag})
		if e != nil {
			return &apiError{409, "CopiedButSourceNotDeleted"}
		}
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) initiate(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, e := key(r)
	if e != nil {
		return e
	}
	defer func() { s.record(r, "INITIATE_MULTIPART", r.PathValue("bucket"), *k, err) }()
	o, e := c.CreateMultipartUpload(r.Context(), &s3.CreateMultipartUploadInput{Bucket: bucket(r), Key: k, ContentType: ptr(r.URL.Query().Get("contentType"))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func multipartParams(r *http.Request) (*string, *string, error) {
	k, e := key(r)
	if e != nil {
		return nil, nil, e
	}
	u := r.URL.Query().Get("uploadId")
	if u == "" {
		return nil, nil, bad("InvalidUploadId")
	}
	return k, &u, nil
}

const maxPartSize = 64 << 20 // Per-request memory ceiling; browser adapts up to this size.
func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request) error {
	select {
	case s.transferSlots <- struct{}{}:
		defer func() { <-s.transferSlots }()
	default:
		return &apiError{429, "TransferBusy"}
	}
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, u, e := multipartParams(r)
	if e != nil {
		return e
	}
	n, e := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if e != nil || n < 1 || n > 10000 {
		return bad("InvalidPart")
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPartSize))
	if e != nil {
		return &apiError{413, "PartTooLarge"}
	}
	o, e := c.UploadPart(r.Context(), &s3.UploadPartInput{Bucket: bucket(r), Key: k, UploadId: u, PartNumber: aws.Int32(int32(n)), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body)))})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) complete(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, u, e := multipartParams(r)
	if e != nil {
		return e
	}
	var v struct {
		Parts []types.CompletedPart `json:"parts"`
	}
	if e = decode(w, r, &v); e != nil {
		return e
	}
	if len(v.Parts) == 0 || len(v.Parts) > 10000 {
		return bad("InvalidPart")
	}
	last := int32(0)
	for _, p := range v.Parts {
		if p.PartNumber == nil || *p.PartNumber <= last || *p.PartNumber > 10000 || aws.ToString(p.ETag) == "" {
			return bad("InvalidPartOrder")
		}
		last = *p.PartNumber
	}
	defer func() { s.record(r, "COMPLETE_MULTIPART", r.PathValue("bucket"), *k, err) }()
	o, e := c.CompleteMultipartUpload(r.Context(), &s3.CompleteMultipartUploadInput{Bucket: bucket(r), Key: k, UploadId: u, MultipartUpload: &types.CompletedMultipartUpload{Parts: v.Parts}})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) abort(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, u, e := multipartParams(r)
	if e != nil {
		return e
	}
	defer func() { s.record(r, "ABORT_MULTIPART", r.PathValue("bucket"), *k, err) }()
	_, e = c.AbortMultipartUpload(r.Context(), &s3.AbortMultipartUploadInput{Bucket: bucket(r), Key: k, UploadId: u})
	if e != nil {
		return e
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) uploads(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	q := r.URL.Query()
	o, e := c.ListMultipartUploads(r.Context(), &s3.ListMultipartUploadsInput{Bucket: bucket(r), Prefix: ptr(q.Get("prefix")), KeyMarker: ptr(q.Get("keyMarker")), UploadIdMarker: ptr(q.Get("uploadIdMarker")), MaxUploads: aws.Int32(100)})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) parts(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	k, u, e := multipartParams(r)
	if e != nil {
		return e
	}
	o, e := c.ListParts(r.Context(), &s3.ListPartsInput{Bucket: bucket(r), Key: k, UploadId: u, PartNumberMarker: ptr(r.URL.Query().Get("partNumberMarker")), MaxParts: aws.Int32(1000)})
	if e != nil {
		return e
	}
	writeJSON(w, 200, o)
	return nil
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	q := r.URL.Query()
	b := q.Get("bucket")
	if b == "" {
		return bad("InvalidBucketName")
	}
	n, e := pageSize(r)
	if e != nil {
		return e
	}
	// A search scans one S3 page at a time. Never claim page-local matches are exhaustive.
	o, e := c.ListObjectsV2(r.Context(), &s3.ListObjectsV2Input{Bucket: &b, Prefix: ptr(q.Get("prefix")), ContinuationToken: ptr(q.Get("token")), MaxKeys: aws.Int32(n)})
	if e != nil {
		return e
	}
	matches := []types.Object{}
	for _, o := range o.Contents {
		if strings.Contains(strings.ToLower(aws.ToString(o.Key)), strings.ToLower(q.Get("q"))) {
			matches = append(matches, o)
		}
	}
	writeJSON(w, 200, map[string]any{"Contents": matches, "IsTruncated": o.IsTruncated, "NextContinuationToken": o.NextContinuationToken})
	return nil
}
