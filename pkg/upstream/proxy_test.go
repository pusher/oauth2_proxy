package upstream

import (
	"crypto"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	middlewareapi "github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/middleware"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/options"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Proxy Suite", func() {
	type proxyTableInput struct {
		target    string
		response  testHTTPResponse
		upstream  string
		upstreams options.Upstreams
	}

	var httpResponse404 = testHTTPResponse{
		code: 404,
		header: map[string][]string{
			"X-Content-Type-Options": {"nosniff"},
			contentType:              {textPlainUTF8},
		},
		raw: "404 page not found\n",
	}
	var httpResponse200Authenticated = testHTTPResponse{
		code:   200,
		header: map[string][]string{},
		raw:    "Authenticated",
	}

	DescribeTable("Proxy ServeHTTP",
		func(in *proxyTableInput) {
			sigData := &options.SignatureData{Hash: crypto.SHA256, Key: "secret"}

			errorHandler := func(rw http.ResponseWriter, _ *http.Request, _ error) {
				rw.WriteHeader(502)
				rw.Write([]byte("Proxy Error"))
			}

			ok := http.StatusOK

			// Allows for specifying settings and even individual upstreams for specific tests and uses the default upstreams/configs otherwise
			upstreams := in.upstreams
			if len(in.upstreams.Configs) == 0 {
				upstreams.Configs = []options.Upstream{
					{
						ID:   "http-backend",
						Path: "/http/",
						URI:  serverAddr,
					},
					{
						ID:   "file-backend",
						Path: "/files/",
						URI:  fmt.Sprintf("file:///%s", filesDir),
					},
					{
						ID:         "static-backend",
						Path:       "/static/",
						Static:     true,
						StaticCode: &ok,
					},
					{
						ID:   "bad-http-backend",
						Path: "/bad-http/",
						URI:  "http://::1",
					},
					{
						ID:         "single-path-backend",
						Path:       "/single-path",
						Static:     true,
						StaticCode: &ok,
					},
				}
			}

			var err error
			upstreamServer, err := NewProxy(upstreams, sigData, errorHandler)
			Expect(err).ToNot(HaveOccurred())

			req := middlewareapi.AddRequestScope(
				httptest.NewRequest("", in.target, nil),
				&middlewareapi.RequestScope{},
			)
			rw := httptest.NewRecorder()
			// Don't mock the remote Address
			req.RemoteAddr = ""

			upstreamServer.ServeHTTP(rw, req)

			scope := middlewareapi.GetRequestScope(req)
			Expect(scope.Upstream).To(Equal(in.upstream))

			Expect(rw.Code).To(Equal(in.response.code))

			// Delete extra headers that aren't relevant to tests
			testSanitizeResponseHeader(rw.Header())
			Expect(rw.Header()).To(Equal(in.response.header))

			body := rw.Body.Bytes()
			// If the raw body is set, check that, else check the Request object
			if in.response.raw != "" {
				Expect(string(body)).To(Equal(in.response.raw))
				return
			}

			// Compare the reflected request to the upstream
			request := testHTTPRequest{}
			Expect(json.Unmarshal(body, &request)).To(Succeed())
			testSanitizeRequestHeader(request.Header)
			Expect(request).To(Equal(in.response.request))
		},
		Entry("with a request to the HTTP service", &proxyTableInput{
			target: "http://example.localhost/http/1234",
			response: testHTTPResponse{
				code: 200,
				header: map[string][]string{
					contentType: {applicationJSON},
				},
				request: testHTTPRequest{
					Method: "GET",
					URL:    "http://example.localhost/http/1234",
					Header: map[string][]string{
						"Gap-Auth":      {""},
						"Gap-Signature": {"sha256 ofB1u6+FhEUbFLc3/uGbJVkl7GaN4egFqVvyO3+2I1w="},
					},
					Body:       []byte{},
					Host:       "example.localhost",
					RequestURI: "http://example.localhost/http/1234",
				},
			},
			upstream: "http-backend",
		}),
		Entry("with a request to the File backend", &proxyTableInput{
			target: "http://example.localhost/files/foo",
			response: testHTTPResponse{
				code: 200,
				header: map[string][]string{
					contentType: {textPlainUTF8},
				},
				raw: "foo",
			},
			upstream: "file-backend",
		}),
		Entry("with a request to the Static backend", &proxyTableInput{
			target:   "http://example.localhost/static/bar",
			response: httpResponse200Authenticated,
			upstream: "static-backend",
		}),
		Entry("with a request to the bad HTTP backend", &proxyTableInput{
			target: "http://example.localhost/bad-http/bad",
			response: testHTTPResponse{
				code:   502,
				header: map[string][]string{},
				// This tests the error handler
				raw: "Proxy Error",
			},
			upstream: "bad-http-backend",
		}),
		Entry("with a request to the to an unregistered path", &proxyTableInput{
			target:   "http://example.localhost/unregistered",
			response: httpResponse404,
		}),
		Entry("with a request to the to backend registered to a single path", &proxyTableInput{
			target:   "http://example.localhost/single-path",
			response: httpResponse200Authenticated,
			upstream: "single-path-backend",
		}),
		Entry("with a request to the to a subpath of a backend registered to a single path", &proxyTableInput{
			target:   "http://example.localhost/single-path/unregistered",
			response: httpResponse404,
		}),
		Entry("with a request to a path containing an escaped '/' in its name", &proxyTableInput{
			target: "http://example.localhost/%2F/",
			response: testHTTPResponse{
				code: 301, // Default http mux will rewrite this with an 301
				header: map[string][]string{
					"Location":  {"http://example.localhost/"},
					contentType: {htmlPlainUTF8},
				},
				raw: "<a href=\"http://example.localhost/\">Moved Permanently</a>.\n\n",
			},
		}),
		Entry("with a request to a path containing an escaped '/' in its name and enabled raw path proxy", &proxyTableInput{
			upstreams: options.Upstreams{ProxyRawPath: true},
			target:    "http://example.localhost/%2F/",
			response:  httpResponse404,
		}),
	)
})
