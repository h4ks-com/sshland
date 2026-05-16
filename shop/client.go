package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type ShopClient struct {
	BaseURL string
	Bearer  string
	HTTP    *http.Client
}

func NewShopClient(baseURL, bearer string) *ShopClient {
	return &ShopClient{
		BaseURL: baseURL,
		Bearer:  bearer,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

type ArticleSummary struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	PreviewImage string  `json:"previewImage"`
	PriceFrom    float64 `json:"priceFrom"`
}

type ArticleVariantDTO struct {
	ID                   int     `json:"id"`
	SKU                  string  `json:"sku"`
	SizeName             string  `json:"sizeName"`
	AppearanceName       string  `json:"appearanceName"`
	AppearanceColorValue string  `json:"appearanceColorValue"`
	Price                float64 `json:"price"`
	Stock                int     `json:"stock"`
}

type ArticleImageDTO struct {
	ID             int    `json:"id"`
	AppearanceID   int    `json:"appearanceId"`
	AppearanceName string `json:"appearanceName"`
	Perspective    string `json:"perspective"`
	ImageURL       string `json:"imageUrl"`
}

type ArticleDetail struct {
	ID          int                 `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Images      []ArticleImageDTO   `json:"images"`
	Variants    []ArticleVariantDTO `json:"variants"`
}

type ArticlesPage struct {
	Items []ArticleSummary `json:"items"`
	Count int              `json:"count"`
}

type CheckoutCreateResponse struct {
	SessionID              string `json:"sessionId"`
	URL                    string `json:"url"`
	ExternalOrderReference string `json:"externalOrderReference"`
}

type CheckoutStatusResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`        // open | complete | expired
	PaymentStatus string `json:"paymentStatus"` // paid | unpaid | no_payment_required
}

func (c *ShopClient) do(ctx context.Context, method, path string, body, out any) error {
	var bodyR io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		bodyR = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyR)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, truncate(string(respBody), 240))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *ShopClient) ListArticles(ctx context.Context, limit int) (*ArticlesPage, error) {
	path := "/api/articles"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var out ArticlesPage
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ShopClient) GetArticle(ctx context.Context, id int) (*ArticleDetail, error) {
	var out ArticleDetail
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/articles/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ShopClient) CreateCheckout(ctx context.Context, articleID int, sku string, quantity int) (*CheckoutCreateResponse, error) {
	body := map[string]any{
		"articleId": articleID,
		"sku":       sku,
		"quantity":  quantity,
	}
	var out CheckoutCreateResponse
	if err := c.do(ctx, http.MethodPost, "/api/checkout", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ShopClient) GetCheckoutStatus(ctx context.Context, sessionID string) (*CheckoutStatusResponse, error) {
	var out CheckoutStatusResponse
	if err := c.do(ctx, http.MethodGet, "/api/checkout/"+sessionID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
