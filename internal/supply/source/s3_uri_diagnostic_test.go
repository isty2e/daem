package source

import (
	"strings"
	"testing"
)

func TestS3URIRejectionReasonsDoNotDependOnRejectedValues(t *testing.T) {
	reasons := make(map[string]string)
	for _, input := range []struct {
		name   string
		values [2]string
	}{
		{name: "required", values: [2]string{"", " \n "}},
		{name: "syntax", values: [2]string{"s3://bucket/private-a%zz", "s3://bucket/private-b%yy"}},
		{name: "scheme", values: [2]string{"https://private-a/object?custom=payload-a", "ftp://private-b/object?custom=payload-b"}},
		{name: "userinfo", values: [2]string{"s3://user:payload-a@private-a/object", "s3://other:payload-b@private-b/object"}},
		{name: "bucket", values: [2]string{"s3:///private-a?custom=payload-a", "s3:///private-b?custom=payload-b"}},
		{name: "query or fragment", values: [2]string{"s3://private-a/object?to\u200bken=payload-a", "s3://private-b/object#custom=payload-b"}},
		{name: "object key", values: [2]string{"s3://private-a", "s3://private-b"}},
		{name: "NUL", values: [2]string{"s3://bucket/private-a%00", "s3://bucket/private-b%00"}},
		{name: "prefix", values: [2]string{"s3://private-a/object/", "s3://private-b/other/"}},
	} {
		t.Run(input.name, func(t *testing.T) {
			var first string
			for index, value := range input.values {
				parsed, err := ParseS3ObjectURI(value)
				if err == nil || parsed != (S3ObjectURI{}) {
					t.Fatal("invalid URI was admitted or returned a nonzero source")
				}
				message := err.Error()
				if index == 0 {
					first = message
				} else if message != first {
					t.Error("rejection reason depends on the rejected URI payload")
				}
				for _, payload := range []string{"private-a", "private-b", "payload-a", "payload-b"} {
					if strings.Contains(message, payload) {
						t.Error("rejection diagnostic echoes URI data")
					}
				}

				constructed, err := NewS3Source(value, "version", "us-east-1", S3ObjectFormatFile)
				if err == nil || constructed != (Source{}) || err.Error() != message {
					t.Error("S3 constructor did not preserve the URI refusal and reason")
				}
			}
			if other, exists := reasons[first]; exists {
				t.Errorf("distinct rejection reasons %q and %q collapsed", other, input.name)
			}
			reasons[first] = input.name
		})
	}
}
