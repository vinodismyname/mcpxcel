package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Unit represents the counting unit used by cursors.
type Unit string

const (
	UnitCells Unit = "cells"
	UnitRows  Unit = "rows"
)

// Cursor is the canonical, opaque pagination token (pre-encoding) with short field names to
// minimize payload size. It is serialized to minified JSON and encoded with URL-safe base64.
//
// Fields:
//   - v:   version of the cursor schema
//   - wid: workbook ID
//   - s:   sheet name
//   - r:   normalized A1 range (no sheet qualifier)
//   - u:   unit: "cells" or "rows"
//   - off: offset in unit from the start of the range/results
//   - ps:  page size in the chosen unit
//   - wbv: workbook write-version snapshot (0 when unavailable)
//   - iat: issued-at timestamp (unix seconds)
//   - qh:  optional query hash (search)
//   - ph:  optional predicate hash (filter)
type Cursor struct {
	V   int    `json:"v"`
	Wid string `json:"wid"`
	S   string `json:"s"`
	R   string `json:"r"`
	U   Unit   `json:"u"`
	Off int    `json:"off"`
	Ps  int    `json:"ps"`
	Wbv int64  `json:"wbv"`
	Iat int64  `json:"iat"`
	Qh  string `json:"qh,omitempty"`
	Ph  string `json:"ph,omitempty"`
	// Optional: carry original search/filter parameters to enable cursor-only resume
	Q  string `json:"q,omitempty"`  // original query for search_data
	Rg bool   `json:"rg,omitempty"` // regex flag for search_data
	Cl []int  `json:"cl,omitempty"` // columns filter for search_data
}

// EncodeCursor serializes and encodes the cursor as URL-safe base64 (without padding).
func EncodeCursor(c Cursor) (string, error) {
	if err := validate(&c); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	return s, nil
}

// DecodeCursor decodes a URL-safe base64 token and parses the JSON cursor.
func DecodeCursor(token string) (*Cursor, error) {
	t := strings.TrimSpace(token)
	if t == "" {
		return nil, errors.New("cursor: empty token")
	}
	data, err := base64.RawURLEncoding.DecodeString(t)
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid base64: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("cursor: invalid json: %w", err)
	}
	if err := validate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// validate performs structural checks and defaulting.
func validate(c *Cursor) error {
	if c.V <= 0 {
		c.V = 1
	}
	if c.Iat == 0 {
		c.Iat = time.Now().Unix()
	}
	if strings.TrimSpace(c.Wid) == "" {
		return errors.New("cursor: wid (workbook id) required")
	}
	if strings.TrimSpace(c.S) == "" {
		return errors.New("cursor: s (sheet) required")
	}
	if strings.TrimSpace(c.R) == "" {
		return errors.New("cursor: r (range) required")
	}
	switch c.U {
	case UnitCells, UnitRows:
		// ok
	default:
		return fmt.Errorf("cursor: invalid unit %q", string(c.U))
	}
	if c.Off < 0 {
		return errors.New("cursor: off must be >= 0")
	}
	if c.Ps <= 0 {
		return errors.New("cursor: ps must be > 0")
	}
	if c.Wbv < 0 {
		c.Wbv = 0
	}
	return nil
}

// NextOffset computes the next offset after returning n units.
func NextOffset(curr, n int) int {
	if curr < 0 {
		curr = 0
	}
	if n <= 0 {
		return curr
	}
	return curr + n
}
