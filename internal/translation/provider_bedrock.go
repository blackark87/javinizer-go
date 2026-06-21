package translation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
)

func (s *Service) translateWithBedrock(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNames []string) (*translationResult, error) {
	cfg := s.cfg.Bedrock
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	accessKeyID := strings.TrimSpace(cfg.AccessKeyID)
	secretAccessKey := strings.TrimSpace(cfg.SecretAccessKey)
	if accessKeyID == "" {
		return nil, fmt.Errorf("bedrock access_key_id is required")
	}
	if secretAccessKey == "" {
		return nil, fmt.Errorf("bedrock secret_access_key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	}

	systemPrompt, userPrompt, markers, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts, fieldNames)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]interface{}{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        4096,
		"system":            systemPrompt,
		"messages":          []map[string]string{{"role": "user", "content": userPrompt}},
	})
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://bedrock-runtime." + region + ".amazonaws.com"
	}
	endpoint := baseURL + "/model/" + url.PathEscape(model) + "/invoke"
	logging.Debugf("Translation (bedrock): POST %s model=%s texts=%d", endpoint, model, len(texts))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	signBedrockRequest(req, body, region, accessKeyID, secretAccessKey, strings.TrimSpace(cfg.SessionToken), time.Now().UTC())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: resp.StatusCode, Message: fmt.Sprintf("bedrock translation failed with status %d: %s", resp.StatusCode, string(respBody))}
	}

	var decoded struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode bedrock response: %w", err)
	}
	if len(decoded.Content) == 0 {
		return nil, fmt.Errorf("bedrock response contained no content blocks")
	}
	return buildLLMTranslationResult(strings.TrimSpace(decoded.Content[0].Text), markers)
}

func (s *Service) translateActressNamesWithBedrock(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	cfg := s.cfg.Bedrock
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	accessKeyID := strings.TrimSpace(cfg.AccessKeyID)
	secretAccessKey := strings.TrimSpace(cfg.SecretAccessKey)
	if accessKeyID == "" {
		return nil, fmt.Errorf("bedrock access_key_id is required")
	}
	if secretAccessKey == "" {
		return nil, fmt.Errorf("bedrock secret_access_key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	}

	systemPrompt, userPrompt, err := buildActressTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]interface{}{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        4096,
		"system":            systemPrompt,
		"messages":          []map[string]string{{"role": "user", "content": userPrompt}},
	})
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://bedrock-runtime." + region + ".amazonaws.com"
	}
	endpoint := baseURL + "/model/" + url.PathEscape(model) + "/invoke"
	logging.Debugf("Translation (bedrock actress): POST %s model=%s texts=%d", endpoint, model, len(texts))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	signBedrockRequest(req, body, region, accessKeyID, secretAccessKey, strings.TrimSpace(cfg.SessionToken), time.Now().UTC())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: resp.StatusCode, Message: fmt.Sprintf("bedrock translation failed with status %d: %s", resp.StatusCode, string(respBody))}
	}

	var decoded struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode bedrock response: %w", err)
	}
	if len(decoded.Content) == 0 {
		return nil, fmt.Errorf("bedrock response contained no content blocks")
	}
	return buildLLMTranslationResult(strings.TrimSpace(decoded.Content[0].Text), makeJZMarkers(len(texts)))
}

func signBedrockRequest(req *http.Request, payload []byte, region, accessKeyID, secretAccessKey, sessionToken string, now time.Time) {
	const service = "bedrock"
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadHash := sha256Hex(payload)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if sessionToken != "" {
		req.Header.Set("x-amz-security-token", sessionToken)
	}
	req.Header.Set("host", req.URL.Host)

	signedHeaders := signedHeaderNames(req.Header)
	canonicalRequest := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.RawQuery, canonicalHeaders(req.Header, signedHeaders), strings.Join(signedHeaders, ";"), payloadHash}, "\n")
	credentialScope := date + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))
	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+secretAccessKey), date), region), service), "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKeyID+"/"+credentialScope+", SignedHeaders="+strings.Join(signedHeaders, ";")+", Signature="+signature)
}

func sha256Hex(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
func signedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, strings.ToLower(n))
	}
	sort.Strings(names)
	return names
}
func canonicalHeaders(h http.Header, names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte(':')
		b.WriteString(strings.Join(h.Values(n), ","))
		b.WriteByte('\n')
	}
	return b.String()
}
