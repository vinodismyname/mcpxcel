package pagination

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeDecodeCursor_RoundTrip(t *testing.T) {
	c := Cursor{
		V:   1,
		Wid: "wb-123",
		S:   "Sheet1",
		R:   "A1:D100",
		U:   UnitCells,
		Off: 200,
		Ps:  1000,
		Wbv: 0,
	}
	tok, err := EncodeCursor(c)
	if err != nil {
		t.Fatalf("EncodeCursor error: %v", err)
	}
	// token should be url-safe base64 (no '+', '/', '=')
	if strings.ContainsAny(tok, "+/=") {
		t.Fatalf("token contains non-url-safe chars: %q", tok)
	}
	out, err := DecodeCursor(tok)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if out.Wid != c.Wid || out.S != c.S || out.R != c.R || out.U != c.U || out.Off != c.Off || out.Ps != c.Ps {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, c)
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	cases := []string{
		"",    // empty
		"!!!", // not base64
		base64.RawURLEncoding.EncodeToString([]byte("not-json")),
		// missing required fields
		mustB64(`{"v":1}`),
		mustB64(`{"v":1,"wid":"x","s":"","r":"A1:B2","u":"cells","off":0,"ps":10}`),
		mustB64(`{"v":1,"wid":"","s":"S","r":"A1:B2","u":"cells","off":0,"ps":10}`),
		mustB64(`{"v":1,"wid":"x","s":"S","r":"","u":"cells","off":0,"ps":10}`),
		mustB64(`{"v":1,"wid":"x","s":"S","r":"A1","u":"bad","off":0,"ps":10}`),
		mustB64(`{"v":1,"wid":"x","s":"S","r":"A1","u":"rows","off":-1,"ps":10}`),
		mustB64(`{"v":1,"wid":"x","s":"S","r":"A1","u":"rows","off":0,"ps":0}`),
	}
	for i, tok := range cases {
		if _, err := DecodeCursor(tok); err == nil {
			t.Fatalf("case %d: expected error for token %q", i, tok)
		}
	}
}

func FuzzDecodeCursor(f *testing.F) {
	seeds := []string{
		"", "abc", mustB64(`{"v":1}`), mustB64(`{"wid":"x"}`),
		mustB64(`{"v":1,"wid":"wb","s":"S","r":"A1","u":"cells","off":0,"ps":1}`),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, token string) {
		_, _ = DecodeCursor(token)
	})
}

func mustB64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
