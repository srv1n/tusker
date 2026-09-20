package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	devinDefaultBaseURL   = "https://api.devin.ai"
	devinSessionIDPrefix  = "devin-"
	devinRequestTimeout   = 30 * time.Second
	devinMessagesPageSize = 200
	devinMaxMessagePages  = 20
	workerDeliveryTagFmt  = "[Tusker delivery %s]"
)

type DevinHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type DevinSessionsClient struct {
	BaseURL    string
	OrgID      string
	HTTPClient DevinHTTPClient
	apiKey     string
	Timeout    time.Duration
	afterFetch func()
}

func (c *DevinSessionsClient) boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = devinRequestTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func NewDevinSessionsClient() (*DevinSessionsClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("DEVIN_API_KEY"))
	orgID := strings.TrimSpace(os.Getenv("DEVIN_ORG_ID"))
	if apiKey == "" || orgID == "" {
		return nil, tuskerError(errorInvalidArg, "devin provider requires DEVIN_API_KEY and DEVIN_ORG_ID")
	}
	return newDevinSessionsClient(strings.TrimSpace(os.Getenv("DEVIN_API_BASE_URL")), orgID, apiKey, nil), nil
}

func newDevinSessionsClient(baseURL, orgID, apiKey string, httpClient DevinHTTPClient) *DevinSessionsClient {
	if baseURL == "" {
		baseURL = devinDefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &DevinSessionsClient{BaseURL: strings.TrimRight(baseURL, "/"), OrgID: orgID, apiKey: apiKey, HTTPClient: httpClient, Timeout: devinRequestTimeout}
}

func validDevinSessionID(sessionID string) bool {
	return strings.HasPrefix(sessionID, devinSessionIDPrefix) && len(sessionID) > len(devinSessionIDPrefix)
}

func (c *DevinSessionsClient) newRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Request, error) {
	if c == nil || c.apiKey == "" || c.OrgID == "" {
		return nil, tuskerError(errorInvalidArg, "devin sessions client is not configured")
	}
	endpoint := c.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *DevinSessionsClient) sessionPath(sessionID string) (string, error) {
	if !validDevinSessionID(sessionID) {
		return "", tuskerError(errorInvalidArg, "devin session id must be prefixed devin-")
	}
	return "/v3/organizations/" + url.PathEscape(c.OrgID) + "/sessions/" + url.PathEscape(sessionID), nil
}

func decodeJSONResponse(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("devin response is not JSON: %w", err)
	}
	return decoded, nil
}

func devinTimestamp(value any) string {
	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0).UTC().Format(time.RFC3339Nano)
	case json.Number:
		if secs, err := typed.Int64(); err == nil {
			return time.Unix(secs, 0).UTC().Format(time.RFC3339Nano)
		}
		if secs, err := strconv.ParseFloat(typed.String(), 64); err == nil {
			return time.Unix(int64(secs), 0).UTC().Format(time.RFC3339Nano)
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return ""
		}
		if parsed, ok := parseWorkerTimestamp(trimmed); ok {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
		if secs, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func devinStringField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if s, ok := value.(string); ok {
				return s
			}
		}
	}
	return ""
}

type DevinSessionStatus struct {
	SessionID string
	Status    string
	UpdatedAt string
	ACUs      float64
}

func (c *DevinSessionsClient) GetSession(ctx context.Context, sessionID string) (DevinSessionStatus, error) {
	path, err := c.sessionPath(sessionID)
	if err != nil {
		return DevinSessionStatus{}, err
	}
	ctx, cancel := c.boundedContext(ctx)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return DevinSessionStatus{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DevinSessionStatus{}, err
	}
	payload, err := decodeJSONResponse(resp)
	if err != nil {
		return DevinSessionStatus{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DevinSessionStatus{}, fmt.Errorf("devin get session returned status %d", resp.StatusCode)
	}
	status := DevinSessionStatus{
		SessionID: firstNonEmpty(devinStringField(payload, "session_id", "id"), sessionID),
		Status:    devinStringField(payload, "status", "status_enum", "state"),
		UpdatedAt: devinTimestamp(devinFieldValue(payload["updated_at"], payload["last_event_at"], payload["created_at"])),
	}
	if value, ok := payload["acus_consumed"].(float64); ok {
		status.ACUs = value
	} else if value, ok := payload["acu_consumed"].(float64); ok {
		status.ACUs = value
	}
	return status, nil
}

func devinFieldValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type DevinSessionMessage struct {
	EventID   string
	Source    string
	Text      string
	CreatedAt string
}

type DevinMessagesPage struct {
	Messages    []DevinSessionMessage
	EndCursor   string
	HasNextPage bool
}

func (c *DevinSessionsClient) ListMessages(ctx context.Context, sessionID, after string) (DevinMessagesPage, error) {
	path, err := c.sessionPath(sessionID)
	if err != nil {
		return DevinMessagesPage{}, err
	}
	query := url.Values{"first": []string{fmt.Sprintf("%d", devinMessagesPageSize)}}
	if after != "" {
		query.Set("after", after)
	}
	ctx, cancel := c.boundedContext(ctx)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, path+"/messages", query, nil)
	if err != nil {
		return DevinMessagesPage{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DevinMessagesPage{}, err
	}
	payload, err := decodeJSONResponse(resp)
	if err != nil {
		return DevinMessagesPage{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DevinMessagesPage{}, fmt.Errorf("devin list messages returned status %d", resp.StatusCode)
	}
	page := DevinMessagesPage{EndCursor: devinStringField(payload, "end_cursor", "next_cursor", "cursor")}
	if value, ok := payload["has_next_page"].(bool); ok {
		page.HasNextPage = value
	}
	rawMessages, _ := payload["messages"].([]any)
	if rawMessages == nil {
		rawMessages, _ = payload["items"].([]any)
	}
	for _, raw := range rawMessages {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		page.Messages = append(page.Messages, DevinSessionMessage{
			EventID:   devinStringField(entry, "event_id", "id", "message_id"),
			Source:    devinStringField(entry, "source", "type", "role", "origin"),
			Text:      devinStringField(entry, "message", "text", "body", "content"),
			CreatedAt: devinTimestamp(devinFieldValue(entry["created_at"], entry["timestamp"], entry["occurred_at"])),
		})
	}
	return page, nil
}

type DevinSendReceipt struct {
	StatusCode int
	SessionID  string
	Status     string
	UpdatedAt  string
	ACUs       float64
	RequestID  string
}

func (r DevinSendReceipt) asMap() map[string]any {
	receipt := map[string]any{"status_code": r.StatusCode}
	if r.SessionID != "" {
		receipt["session_id"] = r.SessionID
	}
	if r.Status != "" {
		receipt["status"] = r.Status
	}
	if r.UpdatedAt != "" {
		receipt["updated_at"] = r.UpdatedAt
	}
	if r.ACUs != 0 {
		receipt["acus_consumed"] = r.ACUs
	}
	if r.RequestID != "" {
		receipt["request_id"] = r.RequestID
	}
	return receipt
}

func (c *DevinSessionsClient) SendMessage(ctx context.Context, sessionID, body string) (DevinSendReceipt, error) {
	path, err := c.sessionPath(sessionID)
	if err != nil {
		return DevinSendReceipt{}, err
	}
	ctx, cancel := c.boundedContext(ctx)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodPost, path+"/messages", nil, map[string]any{"message": body})
	if err != nil {
		return DevinSendReceipt{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DevinSendReceipt{}, err
	}
	payload, err := decodeJSONResponse(resp)
	if err != nil {
		return DevinSendReceipt{}, err
	}
	receipt := DevinSendReceipt{
		StatusCode: resp.StatusCode,
		SessionID:  devinStringField(payload, "session_id"),
		Status:     devinStringField(payload, "status", "status_enum", "state"),
		UpdatedAt:  devinTimestamp(devinFieldValue(payload["updated_at"], payload["created_at"])),
		RequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
	}
	if value, ok := payload["acus_consumed"].(float64); ok {
		receipt.ACUs = value
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return receipt, fmt.Errorf("devin send message returned status %d", resp.StatusCode)
	}
	return receipt, nil
}

func workerDeliveryTag(deliveryID string) string {
	return fmt.Sprintf(workerDeliveryTagFmt, deliveryID)
}

func workerDeliveryOutbound(deliveryID, body string) string {
	tag := workerDeliveryTag(deliveryID)
	return tag + "\n\n" + body + "\n\nReply with the exact tag " + tag + "."
}

func SendDevinWorkerDelivery(ctx context.Context, store *RuntimeStore, deliveryID string, client *DevinSessionsClient) error {
	delivery, claimed, err := store.ClaimWorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !claimed {
		switch delivery.State {
		case "delivering":
			return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" is already in flight")
		case "accepted", "uncertain", "replied", "applied":
			return nil
		default:
			return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" cannot be sent from state "+delivery.State)
		}
	}
	current, err := store.workerIdentityCurrent(delivery.Identity)
	if err != nil {
		return err
	}
	if !current {
		if markErr := store.markWorkerDeliveryState(deliveryID, map[string]bool{"delivering": true}, "stale", "worker identity is stale or superseded"); markErr != nil {
			return markErr
		}
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" identity is stale or superseded")
	}
	receipt, sendErr := client.SendMessage(ctx, delivery.Identity.NativeSessionID, workerDeliveryOutbound(delivery.DeliveryID, delivery.Body))
	if sendErr != nil {
		reason := "devin send result is ambiguous: " + sendErr.Error()
		if receipt.StatusCode != 0 {
			reason = fmt.Sprintf("devin send rejected with status %d", receipt.StatusCode)
		}
		if markErr := store.MarkWorkerDeliveryUncertain(deliveryID, reason); markErr != nil {
			return markErr
		}
		return fmt.Errorf("devin send is uncertain; no resend will be attempted: %w", sendErr)
	}
	return store.MarkWorkerDeliveryAccepted(deliveryID, receipt.asMap(), workerNow())
}

func ReconcileDevinWorker(ctx context.Context, store *RuntimeStore, identity WorkerAttemptIdentity, client *DevinSessionsClient, now time.Time, interval time.Duration) error {
	if err := identity.validate(); err != nil {
		return err
	}
	current, err := store.workerIdentityCurrent(identity)
	if err != nil {
		return err
	}
	session, err := client.GetSession(ctx, identity.NativeSessionID)
	if err != nil {
		return err
	}
	statusEventID := "session-status:" + firstNonEmpty(session.UpdatedAt, "untracked:"+session.Status)
	if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity:        identity,
		ProviderEventID: statusEventID,
		Status:          session.Status,
		Source:          "status",
		OccurredAt:      session.UpdatedAt,
	}); err != nil {
		return err
	}
	requestCursor, err := store.WorkerProviderCursor(identity)
	if err != nil {
		return err
	}
	var allMessages []DevinSessionMessage
	finalCursor := ""
	for page := 0; page < devinMaxMessagePages; page++ {
		result, err := client.ListMessages(ctx, identity.NativeSessionID, requestCursor)
		if err != nil {
			return err
		}
		allMessages = append(allMessages, result.Messages...)
		for _, message := range result.Messages {
			if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
				Identity:        identity,
				ProviderEventID: message.EventID,
				Source:          message.Source,
				OccurredAt:      message.CreatedAt,
			}); err != nil {
				return err
			}
		}
		if result.HasNextPage && result.EndCursor == requestCursor {
			return tuskerError(errorInvalidTransition, "devin messages pagination repeated a cursor")
		}
		if result.EndCursor != "" && result.EndCursor != requestCursor {
			requestCursor = result.EndCursor
			finalCursor = result.EndCursor
		}
		if !result.HasNextPage {
			break
		}
		if page == devinMaxMessagePages-1 {
			return tuskerError(errorInvalidTransition, "devin messages pagination exceeded the page bound")
		}
	}
	if client.afterFetch != nil {
		client.afterFetch()
	}
	current, err = store.workerIdentityCurrent(identity)
	if err != nil {
		return err
	}
	if current {
		pending, err := store.workerPendingDeliveries(identity, "delivering", "accepted", "uncertain")
		if err != nil {
			return err
		}
		consumed := map[string]bool{}
		for _, delivery := range pending {
			if delivery.CorrelatedReplyEventID != "" {
				consumed[delivery.CorrelatedReplyEventID] = true
			}
		}
		for _, delivery := range pending {
			tag := workerDeliveryTag(delivery.DeliveryID)
			observedAt := delivery.WorkerActivityObservedAt
			if observedAt == "" {
				for _, message := range allMessages {
					if message.Source == "user" && strings.Contains(message.Text, tag) {
						observedAt = message.CreatedAt
						if err := store.markWorkerDeliveryObserved(delivery.DeliveryID, observedAt); err != nil {
							return err
						}
						break
					}
				}
			}
			if observedAt == "" {
				continue
			}
			observed, ok := parseWorkerTimestamp(observedAt)
			if !ok {
				continue
			}
			for _, message := range allMessages {
				if message.Source != "devin" || !strings.Contains(message.Text, tag) {
					continue
				}
				if message.EventID != "" && consumed[message.EventID] {
					continue
				}
				created, ok := parseWorkerTimestamp(message.CreatedAt)
				if !ok || !created.After(observed) {
					continue
				}
				if err := store.MarkWorkerDeliveryReply(delivery.DeliveryID, message.EventID, message.CreatedAt); err != nil {
					return err
				}
				if message.EventID != "" {
					consumed[message.EventID] = true
				}
				break
			}
		}
		if finalCursor != "" {
			current, err = store.workerIdentityCurrent(identity)
			if err != nil {
				return err
			}
			if current {
				if err := store.saveWorkerProviderCursor(identity, finalCursor); err != nil {
					return err
				}
			}
		}
	}
	_, err = store.EvaluateWorkerAttention(identity, now, interval)
	return err
}

func (s *RuntimeStore) markWorkerDeliveryObserved(deliveryID, observedAt string) error {
	delivery, found, err := s.WorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !found {
		return tuskerError(errorNotFound, "worker delivery not found: "+deliveryID)
	}
	current, err := s.workerIdentityCurrent(delivery.Identity)
	if err != nil {
		return err
	}
	if !current {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" identity is stale or superseded")
	}
	return s.transitionWorkerDelivery(deliveryID, map[string]bool{"delivering": true, "accepted": true, "uncertain": true, "replied": true}, func(d *WorkerDelivery) error {
		if d.WorkerActivityObservedAt == "" {
			d.WorkerActivityObservedAt = observedAt
		}
		return nil
	})
}
