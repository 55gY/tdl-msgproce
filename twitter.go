package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iyear/tdl/app/up"
)

const maxTwitterMediaSize int64 = 2 * 1024 * 1024 * 1024

var twitterStatusRegex = regexp.MustCompile(`https?://(?:www\.)?(?:x|twitter)\.com/[^\s/]+/status/(\d+)`)
var twitterGuestAuthorization = "Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"
var twitterGuestTokenCache struct {
	sync.Mutex
	value string
}

// TwitterMedia 描述一条 Tweet 中可上传的单个媒体。
type TwitterMedia struct {
	URL      string
	Type     string
	Filename string
	Index    int
}

func extractTwitterLinks(text string) []string {
	matches := twitterStatusRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]struct{}, len(matches))
	links := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 0 {
			continue
		}
		link := strings.TrimRight(match[0], ".,!?;:)]}")
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		links = append(links, link)
	}
	return links
}

func twitterTweetID(link string) (string, error) {
	match := twitterStatusRegex.FindStringSubmatch(link)
	if len(match) != 2 {
		return "", fmt.Errorf("不是有效的 Twitter/X status 链接")
	}
	return match[1], nil
}

func createTwitterQueryURL(tweetID string) string {
	variables := `{"tweetId":"` + tweetID + `","withCommunity":false,"includePromotedContent":false,"withVoice":false}`
	features := `{"creator_subscriptions_tweet_preview_api_enabled":true,"premium_content_api_read_enabled":false,"communities_web_enable_tweet_community_results_fetch":true,"c9s_tweet_anatomy_moderator_badge_enabled":true,"responsive_web_grok_analyze_button_fetch_trends_enabled":false,"responsive_web_grok_analyze_post_followups_enabled":false,"responsive_web_jetfuel_frame":false,"responsive_web_grok_share_attachment_enabled":true,"articles_preview_enabled":true,"responsive_web_edit_tweet_api_enabled":true,"graphql_is_translatable_rweb_tweet_is_translatable_enabled":true,"view_counts_everywhere_api_enabled":true,"longform_notetweets_consumption_enabled":true,"responsive_web_twitter_article_tweet_consumption_enabled":true,"tweet_awards_web_tipping_enabled":false,"creator_subscriptions_quote_tweet_preview_enabled":false,"freedom_of_speech_not_reach_fetch_enabled":true,"standardized_nudges_misinfo":true,"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled":true,"longform_notetweets_rich_text_read_enabled":true,"longform_notetweets_inline_media_enabled":true,"profile_label_improvements_pcf_label_in_post_enabled":true,"rweb_tipjar_consumption_enabled":true,"verified_phone_label_enabled":false,"responsive_web_grok_image_annotation_enabled":true,"responsive_web_graphql_skip_user_profile_image_extensions_enabled":false,"responsive_web_graphql_timeline_navigation_enabled":true,"responsive_web_enhance_cards_enabled":false}`
	fieldToggles := `{"withArticleRichContentState":true,"withArticlePlainText":false}`
	values := url.Values{}
	values.Set("variables", variables)
	values.Set("features", features)
	values.Set("fieldToggles", fieldToggles)
	return "https://x.com/i/api/graphql/zAz9764BcLZOJ0JU2wrd1A/TweetResultByRestId?" + values.Encode()
}

func twitterGuestToken(ctx context.Context) (string, error) {
	if token := os.Getenv("X_GUEST_TOKEN"); token != "" {
		return token, nil
	}
	twitterGuestTokenCache.Lock()
	defer twitterGuestTokenCache.Unlock()
	if twitterGuestTokenCache.value != "" {
		return twitterGuestTokenCache.value, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.com/1.1/guest/activate.json", nil)
	if err != nil {
		return "", fmt.Errorf("创建 X guest token 请求失败: %w", err)
	}
	req.Header.Set("Authorization", twitterGuestAuthorization)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tdl-msgproce)")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 X guest token 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取 X guest token 返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		GuestToken string `json:"guest_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析 X guest token 响应失败: %w", err)
	}
	if result.GuestToken == "" {
		return "", fmt.Errorf("X guest token 响应为空")
	}
	twitterGuestTokenCache.value = result.GuestToken
	return result.GuestToken, nil
}

func invalidateTwitterGuestToken() {
	if os.Getenv("X_GUEST_TOKEN") != "" {
		return
	}
	twitterGuestTokenCache.Lock()
	twitterGuestTokenCache.value = ""
	twitterGuestTokenCache.Unlock()
}

func (p *MessageProcessor) twitterCredentials() (authToken, csrfToken string) {
	authToken = os.Getenv("X_AUTH_TOKEN")
	if authToken == "" {
		authToken = p.config.Twitter.AuthToken
	}
	csrfToken = os.Getenv("X_CSRF_TOKEN")
	if csrfToken == "" {
		csrfToken = p.config.Twitter.CSRFToken
	}
	return authToken, csrfToken
}

func (p *MessageProcessor) fetchTwitterJSON(ctx context.Context, tweetID string) (map[string]any, error) {
	authToken, csrfToken := p.twitterCredentials()
	guestToken := ""
	var err error
	if authToken == "" {
		guestToken, err = twitterGuestToken(ctx)
		if err != nil {
			return nil, err
		}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, createTwitterQueryURL(tweetID), nil)
		if err != nil {
			return nil, fmt.Errorf("创建 X 请求失败: %w", err)
		}
		req.Header.Set("Authorization", twitterGuestAuthorization)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tdl-msgproce)")
		req.Header.Set("x-twitter-client-language", "en")
		req.Header.Set("x-twitter-active-user", "yes")
		if authToken != "" {
			req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", authToken, csrfToken))
			req.Header.Set("x-twitter-auth-type", "OAuth2Session")
		} else if guestToken != "" {
			req.Header.Set("x-guest-token", guestToken)
		}
		if csrfToken != "" {
			req.Header.Set("x-csrf-token", csrfToken)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("请求 X GraphQL 失败: %w", err)
		}
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && attempt == 0 && authToken == "" && os.Getenv("X_GUEST_TOKEN") == "" {
			resp.Body.Close()
			invalidateTwitterGuestToken()
			guestToken, err = twitterGuestToken(ctx)
			if err != nil {
				return nil, err
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if authToken != "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
				return nil, fmt.Errorf("X 登录 Cookie 无效或已过期 (HTTP %d)", resp.StatusCode)
			}
			return nil, fmt.Errorf("X GraphQL 返回 HTTP %d", resp.StatusCode)
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("解析 X GraphQL 响应失败: %w", err)
		}
		if result["errors"] != nil {
			return nil, fmt.Errorf("X GraphQL 返回错误: %v", result["errors"])
		}
		if isTwitterTombstone(result) {
			return nil, fmt.Errorf("推文不可用或仅对登录用户可见")
		}
		return result, nil
	}
	return nil, fmt.Errorf("X GraphQL 请求失败")
}

func isTwitterTombstone(payload map[string]any) bool {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return false
	}
	result, ok := data["tweetResult"].(map[string]any)
	if !ok {
		return false
	}
	item, ok := result["result"].(map[string]any)
	if !ok {
		return false
	}
	typename, _ := item["__typename"].(string)
	return typename == "TweetTombstone"
}

func parseTwitterMedia(payload map[string]any) ([]TwitterMedia, error) {
	var media []TwitterMedia
	seen := make(map[string]struct{})
	var walk func(any)
	walk = func(node any) {
		obj, ok := node.(map[string]any)
		if !ok {
			if list, ok := node.([]any); ok {
				for _, item := range list {
					walk(item)
				}
			}
			return
		}
		legacy, _ := obj["legacy"].(map[string]any)
		if entities, ok := legacy["extended_entities"].(map[string]any); ok {
			if items, ok := entities["media"].([]any); ok {
				for index, raw := range items {
					item, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					mediaURL, mediaType, err := twitterMediaURL(item)
					if err != nil {
						continue
					}
					if _, exists := seen[mediaURL]; exists {
						continue
					}
					seen[mediaURL] = struct{}{}
					media = append(media, TwitterMedia{URL: mediaURL, Type: mediaType, Filename: twitterFilename(mediaURL, mediaType, index), Index: index})
				}
			}
		}
		for _, child := range obj {
			walk(child)
		}
	}
	walk(payload)
	if len(media) == 0 {
		return nil, fmt.Errorf("推文中没有可下载的媒体，或 X 返回了受限/失效内容")
	}
	return media, nil
}

func twitterMediaURL(media map[string]any) (string, string, error) {
	mediaType, _ := media["type"].(string)
	if videoInfo, ok := media["video_info"].(map[string]any); ok {
		variants, _ := videoInfo["variants"].([]any)
		var bestURL string
		var bestBitrate float64 = -1
		for _, raw := range variants {
			variant, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			variantURL, _ := variant["url"].(string)
			contentType, _ := variant["content_type"].(string)
			if variantURL == "" || strings.Contains(contentType, "mpegURL") {
				continue
			}
			bitrate, _ := variant["bitrate"].(float64)
			if bitrate > bestBitrate {
				bestBitrate, bestURL = bitrate, variantURL
			}
		}
		if bestURL == "" {
			return "", "", fmt.Errorf("没有可用的 MP4 视频变体")
		}
		return bestURL, "video", nil
	}
	imageURL, _ := media["media_url_https"].(string)
	if imageURL == "" {
		return "", "", fmt.Errorf("媒体没有图片地址")
	}
	if !strings.Contains(imageURL, "?format=") {
		parts := strings.Split(imageURL, ".")
		if len(parts) > 1 {
			ext := parts[len(parts)-1]
			imageURL = strings.Join(parts[:len(parts)-1], ".") + "?format=" + ext + "&name=orig"
		}
	}
	if mediaType == "" {
		mediaType = "photo"
	}
	return imageURL, mediaType, nil
}

func twitterFilename(mediaURL, mediaType string, index int) string {
	ext := filepath.Ext(strings.Split(mediaURL, "?")[0])
	if ext == "" {
		if mediaType == "video" {
			ext = ".mp4"
		} else {
			ext = ".jpg"
		}
	}
	return fmt.Sprintf("twitter_%d%s", index+1, ext)
}

func probeTwitterMediaSize(ctx context.Context, mediaURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, mediaURL, nil)
	if err != nil {
		return 0, fmt.Errorf("创建大小探测请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tdl-msgproce)")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("大小探测失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("大小探测返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength <= 0 {
		return 0, fmt.Errorf("媒体服务器未返回可靠的 Content-Length，无法预先确认文件大小")
	}
	return resp.ContentLength, nil
}

func downloadTwitterMedia(ctx context.Context, media TwitterMedia, expectedSize int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tdl-msgproce)")
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return "", fmt.Errorf("下载媒体失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载媒体返回 HTTP %d", resp.StatusCode)
	}
	if expectedSize > maxTwitterMediaSize {
		return "", fmt.Errorf("文件大小 %.2f GiB，超过 Telegram 单文件 2GiB 限制", float64(expectedSize)/(1024*1024*1024))
	}
	file, err := os.CreateTemp("", "tdl-twitter-*")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	path := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(path)
		}
	}()
	written, err := io.CopyN(file, io.LimitReader(resp.Body, maxTwitterMediaSize+1), maxTwitterMediaSize+1)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("保存媒体失败: %w", err)
	}
	if written > maxTwitterMediaSize {
		return "", fmt.Errorf("实际下载内容超过 Telegram 单文件 2GiB 限制")
	}
	if expectedSize != written {
		return "", fmt.Errorf("媒体大小发生变化，预检 %d 字节，实际 %d 字节", expectedSize, written)
	}
	cleanup = false
	return path, nil
}

func (p *MessageProcessor) processTwitterLink(ctx context.Context, link, caption string, onProgress func(int, string)) error {
	tweetID, err := twitterTweetID(link)
	if err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(5, "解析 Tweet 媒体")
	}
	payload, err := p.fetchTwitterJSON(ctx, tweetID)
	if err != nil {
		return err
	}
	media, err := parseTwitterMedia(payload)
	if err != nil {
		return err
	}
	for index, item := range media {
		if onProgress != nil {
			onProgress(10+index*80/len(media), fmt.Sprintf("预检第 %d/%d 个媒体", index+1, len(media)))
		}
		size, err := probeTwitterMediaSize(ctx, item.URL)
		if err != nil {
			return fmt.Errorf("第 %d 个媒体已跳过：%w", index+1, err)
		}
		if size > maxTwitterMediaSize {
			return fmt.Errorf("第 %d 个媒体已跳过：文件大小 %.2f GiB，超过 Telegram 单文件 2GiB 限制", index+1, float64(size)/(1024*1024*1024))
		}
		path, err := downloadTwitterMedia(ctx, item, size)
		if err != nil {
			return err
		}
		func() {
			defer os.Remove(path)
			if onProgress != nil {
				onProgress(50+index*45/len(media), fmt.Sprintf("上传第 %d/%d 个媒体", index+1, len(media)))
			}
			photo := item.Type == "photo"
			err = up.Run(ctx, p.client, newMemoryStorage(), up.Options{To: fmt.Sprintf("%d", p.config.Bot.ForwardTarget), Paths: []string{path}, Caption: caption, Photo: photo})
		}()
		if err != nil {
			return fmt.Errorf("第 %d 个媒体上传失败: %w", index+1, err)
		}
	}
	if onProgress != nil {
		onProgress(100, "上传完成")
	}
	return nil
}

func extractHashtagCaption(text string) string {
	matches := regexp.MustCompile(`#([^\s#]+)`).FindAllString(text, -1)
	return strings.Join(matches, " ")
}

func taskTypeLabel(taskType string) string {
	if taskType == "twitter_download" {
		return "Twitter 下载"
	}
	return "Telegram 转发"
}
