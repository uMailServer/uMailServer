package jmap

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const fuzzUser = "alice@example.com"

func fuzzServer(f *testing.F) *Server {
	dir, err := os.MkdirTemp("", "jmapfuzz")
	if err != nil {
		f.Fatal(err)
	}
	db, err := storage.OpenDatabase(dir + "/t.db")
	if err != nil {
		f.Fatal(err)
	}
	ms, err := storage.NewMessageStore(dir + "/m")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close(); ms.Close(); os.RemoveAll(dir) })
	_ = db.CreateMailbox(fuzzUser, "INBOX")
	_ = db.CreateMailbox(fuzzUser, "Archive")
	for i := uint32(1); i <= 3; i++ {
		_ = db.StoreMessageMetadata(fuzzUser, "INBOX", i, &storage.MessageMetadata{
			UID: i, MessageID: "m" + string(rune('0'+i)), Subject: "Hello", From: "bob@example.com", Size: 100, Flags: []string{"\\Seen"},
			InternalDate: time.Unix(1700000000, 0),
		})
	}
	return NewServer(db, ms, nil, Config{JWTSecret: "fuzz-secret"})
}

func FuzzJMAPRequest(f *testing.F) {
	srv := fuzzServer(f)
	for _, s := range []string{
		`{"using":["urn:ietf:params:jmap:mail"],"methodCalls":[["Mailbox/get",{"accountId":"alice@example.com"},"a"],["Email/query",{"accountId":"alice@example.com","filter":{"inMailbox":"inbox","text":"hi","notKeyword":"$seen"},"sort":[{"property":"receivedAt","isAscending":false}],"position":-5,"limit":3},"b"],["Email/get",{"accountId":"alice@example.com","#ids":{"resultOf":"b","name":"Email/query","path":"/ids"},"properties":["subject","keywords"]},"c"]]}`,
		`{"methodCalls":[["Email/set",{"accountId":"alice@example.com","update":{"m1":{"keywords/$seen":true,"mailboxIds/archive":true}},"destroy":["m2"],"create":{"k":{"mailboxIds":{"inbox":true}}}},"a"]]}`,
		`{"methodCalls":[["Mailbox/set",{"accountId":"alice@example.com","create":{"x":{"name":"../../etc","parentId":"x"}},"update":{"inbox":{"name":""}},"destroy":["archive"]},"a"]]}`,
		`{"methodCalls":[["Email/import",{"accountId":"alice@example.com","emails":{"i":{"blobId":"zz","mailboxIds":{"inbox":true}}}},"a"],["Thread/get",{"accountId":"alice@example.com","ids":["t1"]},"b"],["SearchSnippet/get",{"accountId":"alice@example.com","emailIds":["m1"],"filter":{"text":"x"}},"c"]]}`,
		`{"methodCalls":[["Email/changes",{"accountId":"alice@example.com","sinceState":"0","maxChanges":-1},"a"],["Email/queryChanges",{"accountId":"alice@example.com","sinceState":"x","upToId":"m1"},"b"],["Identity/set",{"accountId":"alice@example.com","create":{"i":{"email":"a@b.c"}}},"c"]]}`,
		`{"methodCalls":[["Email/query",{"accountId":"alice@example.com","position":-1e300,"limit":1e300,"calculateTotal":true},"a"],["Email/query",{"accountId":"alice@example.com","position":9223372036854775807,"limit":-9223372036854775808},"b"],["Email/query",{"accountId":"alice@example.com","position":1e19,"limit":9007199254740993},"c"],["Thread/query",{"accountId":"alice@example.com","limit":1e30},"d"],["Email/queryChanges",{"accountId":"alice@example.com","sinceState":"18446744073709551615","maxChanges":1e30},"e"],["Email/changes",{"accountId":"alice@example.com","sinceState":"18446744073709551615"},"f"]]}`,
		`{"methodCalls":[{"name":"Mailbox/query","args":{"accountId":"alice@example.com"},"id":"legacy"}]}`,
		`{"methodCalls":[["Email/get",{"accountId":"alice@example.com","ids":null,"#ids":{"resultOf":"a","name":"x","path":"/*/*/*"}},"a"]]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 1<<16 {
			return
		}
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			return
		}
		if len(req.MethodCalls) > maxCallsInRequest {
			req.MethodCalls = req.MethodCalls[:maxCallsInRequest]
		}
		start := time.Now()
		var responses []Response
		for _, call := range req.MethodCalls {
			var resp Response
			if resolved, errResp, ok := resolveResultReferences(call, responses); ok {
				resp = srv.processMethodCall(fuzzUser, resolved)
			} else {
				resp = errResp
			}
			if resp.Name == "" {
				t.Fatalf("empty response name for call %q", call.Name)
			}
			if resp.ID != call.ID {
				t.Fatalf("response id %q != call id %q (%s)", resp.ID, call.ID, call.Name)
			}
			if _, err := json.Marshal(resp); err != nil {
				t.Fatalf("response not encodable: %v (%s)", err, call.Name)
			}
			responses = append(responses, resp)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("request took %v", d)
		}
	})
}

func FuzzJMAPResultPointer(f *testing.F) {
	f.Add(`{"list":[{"id":"a","ids":["x","y"]},{"id":"b"}]}`, "/list/*/id")
	f.Add(`[[[[1]]]]`, "/*/*/*/*")
	f.Add(`{"a~b":{"c/d":1}}`, "/a~0b/c~1d")
	f.Add(`[1,2,3]`, "/99999999999999999999")
	f.Fuzz(func(t *testing.T, doc, path string) {
		var v interface{}
		if json.Unmarshal([]byte(doc), &v) != nil {
			return
		}
		start := time.Now()
		evalResultPointer(v, path)
		if time.Since(start) > time.Second {
			t.Fatalf("slow pointer eval")
		}
		var c MethodCall
		_ = c.UnmarshalJSON([]byte(doc))
		var r Response
		_ = r.UnmarshalJSON([]byte(doc))
		parseFilter(v)
	})
}
