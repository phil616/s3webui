// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/http"
)

// Documents follow the AWS SDK input shape. Bucket is always bound to the route.
func (s *Server) bucketConfig(w http.ResponseWriter, r *http.Request) (err error) {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	if r.Method != "GET" {
		defer func() { s.record(r, r.Method+"_BUCKET_CONFIG", r.PathValue("bucket"), r.PathValue("kind"), err) }()
	}
	var out any
	switch r.PathValue("kind") {
	case "policy":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketPolicy(r.Context(), &s3.GetBucketPolicyInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketPolicyInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketPolicy(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketPolicy(r.Context(), &s3.DeleteBucketPolicyInput{Bucket: bucket(r)})
		}
	case "cors":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketCors(r.Context(), &s3.GetBucketCorsInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketCorsInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketCors(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketCors(r.Context(), &s3.DeleteBucketCorsInput{Bucket: bucket(r)})
		}
	case "versioning":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketVersioning(r.Context(), &s3.GetBucketVersioningInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketVersioningInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketVersioning(r.Context(), &in)
		case "DELETE":
			return &apiError{405, "MethodNotAllowed"}
		}
	case "lifecycle":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketLifecycleConfiguration(r.Context(), &s3.GetBucketLifecycleConfigurationInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketLifecycleConfigurationInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketLifecycleConfiguration(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketLifecycle(r.Context(), &s3.DeleteBucketLifecycleInput{Bucket: bucket(r)})
		}
	case "encryption":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketEncryption(r.Context(), &s3.GetBucketEncryptionInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketEncryptionInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketEncryption(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketEncryption(r.Context(), &s3.DeleteBucketEncryptionInput{Bucket: bucket(r)})
		}
	case "tags":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketTagging(r.Context(), &s3.GetBucketTaggingInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketTaggingInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketTagging(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketTagging(r.Context(), &s3.DeleteBucketTaggingInput{Bucket: bucket(r)})
		}
	case "public-access":
		switch r.Method {
		case "GET":
			out, err = c.GetPublicAccessBlock(r.Context(), &s3.GetPublicAccessBlockInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutPublicAccessBlockInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutPublicAccessBlock(r.Context(), &in)
		case "DELETE":
			out, err = c.DeletePublicAccessBlock(r.Context(), &s3.DeletePublicAccessBlockInput{Bucket: bucket(r)})
		}
	case "ownership":
		switch r.Method {
		case "GET":
			out, err = c.GetBucketOwnershipControls(r.Context(), &s3.GetBucketOwnershipControlsInput{Bucket: bucket(r)})
		case "PUT":
			var in s3.PutBucketOwnershipControlsInput
			if e = decode(w, r, &in); e != nil {
				return e
			}
			in.Bucket = bucket(r)
			out, err = c.PutBucketOwnershipControls(r.Context(), &in)
		case "DELETE":
			out, err = c.DeleteBucketOwnershipControls(r.Context(), &s3.DeleteBucketOwnershipControlsInput{Bucket: bucket(r)})
		}
	default:
		return &apiError{404, "NotFound"}
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
