// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package http

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

func init() {
	internal.Register("tool/http", pkg)
}

var _ = adt.TopKind // in case the adt package isn't used

var pkg = &internal.Package{
	Native: []*internal.Builtin{},
	CUE: `{
	Get: Do & {
		method: "GET"
	}
	Post: Do & {
		method: "POST"
	}
	Put: Do & {
		method: "PUT"
	}
	Delete: Do & {
		method: "DELETE"
	}
	Do: {
		$id:    *"tool/http.Do" | "http"
		method: string
		url:    string
		tls: {
			verify:  *true | bool
			caCert?: bytes | string
		}
		request: {
			body?: bytes | string
			header: {
				[string]: string | [...string]
			}
			trailer: {
				[string]: string | [...string]
			}
		}
		response: {
			status:     string
			statusCode: int
			body:       *bytes | string
			header: {
				[string]: string | [...string]
			}
			trailer: {
				[string]: string | [...string]
			}
		}
	}
}`,
}
