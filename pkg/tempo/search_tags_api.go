package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	apidata "github.com/grafana/grafana-plugin-sdk-go/experimental/apis/datasource/v0alpha1"
)

const (
	tempoMIMETypeJSON              = "application/json"
	tempoPathV2SearchTags          = "api/v2/search/tags"
	tempoPathV2SearchTagPrefix     = "api/v2/search/tag"
	tempoPathTagValuesSuffix       = "values"
	defaultSearchTagsLimit         = 5000
	tempoQueryParamLimit           = "limit"
	tempoQueryParamStart           = "start"
	tempoQueryParamEnd             = "end"
	tempoUnixMillisThreshold       = 1_000_000_000_000
	tempoDefaultTagValuesLookbackSec = 3600
)

type tempoSearchTagsV2Response struct {
	Scopes []tempoSearchTagScope `json:"scopes"`
}

type tempoSearchTagScope struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type tempoTagValuesResponse struct {
	TagValues []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"tagValues"`
}

func buildTempoURL(baseURL, tempoPath string, query url.Values) (*url.URL, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse datasource url: %w", err)
	}
	u.Path = path.Join(u.Path, tempoPath)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u, nil
}

func tempoTagValuesPath(tag string) string {
	return path.Join(tempoPathV2SearchTagPrefix, url.PathEscape(tag), tempoPathTagValuesSuffix)
}

func (ds *DataSource) tempoGET(ctx context.Context, dsInfo *DatasourceInfo, tempoPath string, query url.Values) ([]byte, error) {
	u, err := buildTempoURL(dsInfo.URL, tempoPath, query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", tempoMIMETypeJSON)

	resp, err := dsInfo.HTTPClient.Do(req) // #nosec G704 -- URL comes from operator-configured datasource
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("tempo request %s: %s: %s", tempoPath, resp.Status, string(body))
	}
	return body, nil
}

func (ds *DataSource) fetchSearchTagScopes(ctx context.Context, dsInfo *DatasourceInfo, limit int) ([]tempoSearchTagScope, error) {
	q := url.Values{}
	q.Set(tempoQueryParamLimit, strconv.Itoa(limit))

	body, err := ds.tempoGET(ctx, dsInfo, tempoPathV2SearchTags, q)
	if err != nil {
		return nil, err
	}

	var parsedBody tempoSearchTagsV2Response
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		return nil, fmt.Errorf("decode tags response: %w", err)
	}
	return parsedBody.Scopes, nil
}

func (ds *DataSource) fetchTagValuesForColumn(ctx context.Context, dsInfo *DatasourceInfo, tag string, tr apidata.TimeRange) ([]string, error) {
	q := url.Values{}
	q.Set(tempoQueryParamLimit, strconv.Itoa(defaultSearchTagsLimit))
	start, end := timeRangeToUnixForTempoTagAPI(tr)
	q.Set(tempoQueryParamStart, strconv.FormatInt(start, 10))
	q.Set(tempoQueryParamEnd, strconv.FormatInt(end, 10))

	body, err := ds.tempoGET(ctx, dsInfo, tempoTagValuesPath(tag), q)
	if err != nil {
		return nil, err
	}

	var parsed tempoTagValuesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode tag values: %w", err)
	}
	seen := make(map[string]struct{})
	for _, tv := range parsed.TagValues {
		if tv.Value == "" {
			continue
		}
		seen[tv.Value] = struct{}{}
	}
	vals := make([]string, 0, len(seen))
	for v := range seen {
		vals = append(vals, v)
	}
	sort.Strings(vals)
	return vals, nil
}

func timeRangeToUnixForTempoTagAPI(tr apidata.TimeRange) (start, end int64) {
	fromS := strings.TrimSpace(tr.From)
	toS := strings.TrimSpace(tr.To)
	if fromS == "" || toS == "" {
		now := time.Now().Unix()
		return now - tempoDefaultTagValuesLookbackSec, now
	}
	fromT, err1 := parseFlexibleTimeForTagValues(fromS)
	toT, err2 := parseFlexibleTimeForTagValues(toS)
	if err1 != nil || err2 != nil {
		now := time.Now().Unix()
		return now - tempoDefaultTagValuesLookbackSec, now
	}
	return fromT.Unix(), toT.Unix()
}

func parseFlexibleTimeForTagValues(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ms > tempoUnixMillisThreshold {
			return time.UnixMilli(ms), nil
		}
		return time.Unix(ms, 0), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %q", s)
}
