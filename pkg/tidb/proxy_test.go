package tidb

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProxy(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	want := "hello proxy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(want))
		if err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(l, map[string]string{"test": fmt.Sprintf("%s:%s", u.Hostname(), u.Port())}, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	go p.run(ctx)
	defer cancel()

	u.Host = l.Addr().String()
	res, err := http.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, want, string(got))
}

func TestProxyPick(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	n := 3
	responseData := "test"
	endpoints := make(map[string]string)
	picked := make(map[int]bool)
	servers := make(map[int]*httptest.Server)
	var currentPicked int
	for i := 0; i < n; i++ {
		idx := i
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			picked[idx] = true
			currentPicked = idx
			_, err := w.Write([]byte(responseData))
			if err != nil {
				t.Fatal(err)
			}
		}))
		defer server.Close()
		u, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		key := strconv.Itoa(i)
		endpoint := fmt.Sprintf("%s:%s", u.Hostname(), u.Port())
		endpoints[key] = endpoint
		servers[idx] = server
	}
	p := newProxy(l, endpoints, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	go p.run(ctx)
	defer cancel()

	for i := 0; i < n; i++ {
		client := &http.Client{}
		res, err := client.Get("http://" + l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_, err = ioutil.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		// close conn manually to force proxy re-pick remote
		client.CloseIdleConnections()
		time.Sleep(time.Second)
	}
	// Always pick the same active remote
	assert.Equal(t, 1, len(picked))
	ps := servers[currentPicked]
	if ps == nil {
		t.Fatal("Fail to get current picked server")
	}
	// Shutdown current server to see if we can pick a new one
	ps.Close()
	client := &http.Client{}
	res, err := client.Get("http://" + l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, responseData, string(data))
}