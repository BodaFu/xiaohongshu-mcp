package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// MCP 工具处理函数

// handleCheckLoginStatus 处理检查登录状态
func (s *AppServer) handleCheckLoginStatus(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 检查登录状态")

	status, err := s.xiaohongshuService.CheckLoginStatus(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "检查登录状态失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 根据 IsLoggedIn 判断并返回友好的提示
	var resultText string
	if status.IsLoggedIn {
		resultText = fmt.Sprintf("✅ 已登录\n用户名: %s\n\n你可以使用其他功能了。", status.Username)
	} else {
		resultText = fmt.Sprintf("❌ 未登录\n\n请使用 get_login_qrcode 工具获取二维码进行登录。")
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleGetLoginQrcode 处理获取登录二维码请求。
// 返回二维码图片的 Base64 编码和超时时间，供前端展示扫码登录。
func (s *AppServer) handleGetLoginQrcode(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 获取登录扫码图片")

	result, err := s.xiaohongshuService.GetLoginQrcode(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取登录扫码图片失败: " + err.Error()}},
			IsError: true,
		}
	}

	if result.IsLoggedIn {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "你当前已处于登录状态"}},
		}
	}

	now := time.Now()
	deadline := func() string {
		d, err := time.ParseDuration(result.Timeout)
		if err != nil {
			return now.Format("2006-01-02 15:04:05")
		}
		return now.Add(d).Format("2006-01-02 15:04:05")
	}()

	// 已登录：文本 + 图片
	contents := []MCPContent{
		{Type: "text", Text: "请用小红书 App 在 " + deadline + " 前扫码登录 👇"},
		{
			Type:     "image",
			MimeType: "image/png",
			Data:     strings.TrimPrefix(result.Img, "data:image/png;base64,"),
		},
	}
	return &MCPToolResult{Content: contents}
}

// handleDeleteCookies 处理删除 cookies 请求，用于登录重置
func (s *AppServer) handleDeleteCookies(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 删除 cookies，重置登录状态")

	err := s.xiaohongshuService.DeleteCookies(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "删除 cookies 失败: " + err.Error()}},
			IsError: true,
		}
	}

	cookiePath := cookies.GetCookiesFilePath()
	resultText := fmt.Sprintf("Cookies 已成功删除，登录状态已重置。\n\n删除的文件路径: %s\n\n下次操作时，需要重新登录。", cookiePath)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handlePublishContent 处理发布内容
func (s *AppServer) handlePublishContent(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发布内容")

	// 解析参数
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	imagePathsInterface, _ := args["images"].([]interface{})
	tagsInterface, _ := args["tags"].([]interface{})

	var imagePaths []string
	for _, path := range imagePathsInterface {
		if pathStr, ok := path.(string); ok {
			imagePaths = append(imagePaths, pathStr)
		}
	}

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	// 解析定时发布参数
	scheduleAt, _ := args["schedule_at"].(string)

	logrus.Infof("MCP: 发布内容 - 标题: %s, 图片数量: %d, 标签数量: %d, 定时: %s", title, len(imagePaths), len(tags), scheduleAt)

	// 构建发布请求
	req := &PublishRequest{
		Title:      title,
		Content:    content,
		Images:     imagePaths,
		Tags:       tags,
		ScheduleAt: scheduleAt,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishContent(ctx, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("内容发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handlePublishVideo 处理发布视频内容（仅本地单个视频文件）
func (s *AppServer) handlePublishVideo(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发布视频内容（本地）")

	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	videoPath, _ := args["video"].(string)
	tagsInterface, _ := args["tags"].([]interface{})

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	if videoPath == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: 缺少本地视频文件路径",
			}},
			IsError: true,
		}
	}

	// 解析定时发布参数
	scheduleAt, _ := args["schedule_at"].(string)

	logrus.Infof("MCP: 发布视频 - 标题: %s, 标签数量: %d, 定时: %s", title, len(tags), scheduleAt)

	// 构建发布请求
	req := &PublishVideoRequest{
		Title:      title,
		Content:    content,
		Video:      videoPath,
		Tags:       tags,
		ScheduleAt: scheduleAt,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishVideo(ctx, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("视频发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleListFeeds 处理获取Feeds列表
func (s *AppServer) handleListFeeds(ctx context.Context) *MCPToolResult {
	logrus.Info("MCP: 获取Feeds列表")

	result, err := s.xiaohongshuService.ListFeeds(ctx)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feeds列表失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feeds列表成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleSearchFeeds 处理搜索Feeds
func (s *AppServer) handleSearchFeeds(ctx context.Context, args SearchFeedsArgs) *MCPToolResult {
	logrus.Info("MCP: 搜索Feeds")

	if args.Keyword == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: 缺少关键词参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 搜索Feeds - 关键词: %s", args.Keyword)

	// 将 MCP 的 FilterOption 转换为 xiaohongshu.FilterOption
	filter := xiaohongshu.FilterOption{
		SortBy:      args.Filters.SortBy,
		NoteType:    args.Filters.NoteType,
		PublishTime: args.Filters.PublishTime,
		SearchScope: args.Filters.SearchScope,
		Location:    args.Filters.Location,
	}

	result, err := s.xiaohongshuService.SearchFeeds(ctx, args.Keyword, filter)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("搜索Feeds成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleGetFeedDetail 处理获取Feed详情
func (s *AppServer) handleGetFeedDetail(ctx context.Context, args map[string]any) *MCPToolResult {
	logrus.Info("MCP: 获取Feed详情")

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	loadAll := false
	if raw, ok := args["load_all_comments"]; ok {
		switch v := raw.(type) {
		case bool:
			loadAll = v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				loadAll = parsed
			}
		case float64:
			loadAll = v != 0
		}
	}

	// 解析评论配置参数，如果未提供则使用默认值
	config := xiaohongshu.DefaultCommentLoadConfig()

	if raw, ok := args["click_more_replies"]; ok {
		switch v := raw.(type) {
		case bool:
			config.ClickMoreReplies = v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				config.ClickMoreReplies = parsed
			}
		}
	}

	if raw, ok := args["max_replies_threshold"]; ok {
		switch v := raw.(type) {
		case float64:
			config.MaxRepliesThreshold = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				config.MaxRepliesThreshold = parsed
			}
		case int:
			config.MaxRepliesThreshold = v
		}
	}

	if raw, ok := args["max_comment_items"]; ok {
		switch v := raw.(type) {
		case float64:
			config.MaxCommentItems = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				config.MaxCommentItems = parsed
			}
		case int:
			config.MaxCommentItems = v
		}
	}

	if raw, ok := args["scroll_speed"].(string); ok && raw != "" {
		config.ScrollSpeed = raw
	}

	logrus.Infof("MCP: 获取Feed详情 - Feed ID: %s, loadAllComments=%v, config=%+v", feedID, loadAll, config)

	result, err := s.xiaohongshuService.GetFeedDetailWithConfig(ctx, feedID, xsecToken, loadAll, config)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feed详情成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleUserProfile 获取用户主页
func (s *AppServer) handleUserProfile(ctx context.Context, args map[string]any) *MCPToolResult {
	logrus.Info("MCP: 获取用户主页")

	// 解析参数
	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少user_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 获取用户主页 - User ID: %s", userID)

	result, err := s.xiaohongshuService.UserProfile(ctx, userID, xsecToken)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取用户主页，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleLikeFeed 处理点赞/取消点赞
func (s *AppServer) handleLikeFeed(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unlike, _ := args["unlike"].(bool)

	var res *ActionResult
	var err error

	if unlike {
		res, err = s.xiaohongshuService.UnlikeFeed(ctx, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.LikeFeed(ctx, feedID, xsecToken)
	}

	if err != nil {
		action := "点赞"
		if unlike {
			action = "取消点赞"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "点赞"
	if unlike {
		action = "取消点赞"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handleFavoriteFeed 处理收藏/取消收藏
func (s *AppServer) handleFavoriteFeed(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unfavorite, _ := args["unfavorite"].(bool)

	var res *ActionResult
	var err error

	if unfavorite {
		res, err = s.xiaohongshuService.UnfavoriteFeed(ctx, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.FavoriteFeed(ctx, feedID, xsecToken)
	}

	if err != nil {
		action := "收藏"
		if unfavorite {
			action = "取消收藏"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "收藏"
	if unfavorite {
		action = "取消收藏"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handlePostComment 处理发表评论到Feed
func (s *AppServer) handlePostComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 发表评论到Feed")

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少content参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 发表评论 - Feed ID: %s, 内容长度: %d", feedID, len(content))

	// 发表评论
	result, err := s.xiaohongshuService.PostCommentToFeed(ctx, feedID, xsecToken, content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 返回成功结果，只包含feed_id
	resultText := fmt.Sprintf("评论发表成功 - Feed ID: %s", result.FeedID)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleReplyComment 处理回复评论
func (s *AppServer) handleReplyComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	logrus.Info("MCP: 回复评论")

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	commentID, _ := args["comment_id"].(string)
	userID, _ := args["user_id"].(string)
	if commentID == "" && userID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少comment_id或user_id参数",
			}},
			IsError: true,
		}
	}

	parentCommentID, _ := args["parent_comment_id"].(string)

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少content参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 回复评论 - Feed ID: %s, Comment ID: %s, parent_comment_id: %s, User ID: %s, 内容长度: %d",
		feedID, commentID, parentCommentID, userID, len(content))

	// 回复评论
	result, err := s.xiaohongshuService.ReplyCommentToFeed(ctx, feedID, xsecToken, commentID, userID, parentCommentID, content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 返回成功结果
	responseText := fmt.Sprintf("评论回复成功 - Feed ID: %s, Comment ID: %s, User ID: %s", result.FeedID, result.TargetCommentID, result.TargetUserID)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: responseText,
		}},
	}
}

// handleGetNotifications 处理获取通知列表请求
func (s *AppServer) handleGetNotifications(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	cursor, _ := args["cursor"].(string)
	limitFloat, _ := args["limit"].(float64)
	limit := int(limitFloat)
	if limit <= 0 {
		limit = 20
	}
	sinceUnix, _ := args["since_unix"].(int64)

	logrus.Infof("MCP: 获取通知列表 - cursor=%s, limit=%d, since_unix=%d", cursor, limit, sinceUnix)

	var result *xiaohongshu.NotificationsResult
	var err error

	if sinceUnix > 0 {
		result, err = s.xiaohongshuService.GetNotificationsSince(ctx, sinceUnix)
	} else {
		result, err = s.xiaohongshuService.GetNotifications(ctx, cursor, limit)
	}
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取通知失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	if len(result.Notifications) == 0 {
		msg := "暂无通知"
		if cursor != "" {
			msg = "已到最后一页，没有更多旧通知"
		}
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: msg}},
		}
	}

	var sb strings.Builder
	cst := time.FixedZone("CST", 8*3600)

	sb.WriteString(fmt.Sprintf("共 %d 条通知", len(result.Notifications)))
	if len(result.Notifications) > 0 {
		t0 := time.Unix(result.Notifications[0].Time, 0).In(cst)
		tN := time.Unix(result.Notifications[len(result.Notifications)-1].Time, 0).In(cst)
		sb.WriteString(fmt.Sprintf("（%s ~ %s）", tN.Format("01-02 15:04"), t0.Format("01-02 15:04")))
	}
	sb.WriteString("\n")
	if result.HasMore {
		sb.WriteString(fmt.Sprintf("next_cursor=%s（传入可获取更早的通知）\n", result.NextCursor))
	} else {
		sb.WriteString("已是最后一页\n")
	}
	sb.WriteString("\n")

	for i, n := range result.Notifications {
		t := time.Unix(n.Time, 0).In(cst)
		timeStr := t.Format("2006-01-02 15:04:05")

		var relationLabel string
		switch n.RelationType {
		case xiaohongshu.RelationCommentOnMyNote:
			relationLabel = "评论了我的笔记"
		case xiaohongshu.RelationReplyToMyComment:
			relationLabel = "回复了我的评论"
		case xiaohongshu.RelationAtOthersUnderMyComment:
			relationLabel = "在我的评论下@了他人"
		case xiaohongshu.RelationMentionedMe:
			relationLabel = "在评论中@了我"
		default:
			relationLabel = string(n.RelationType)
		}

		sb.WriteString(fmt.Sprintf("--- 通知 %d [%s] ---\n", i+1, relationLabel))
		sb.WriteString(fmt.Sprintf("notification_id: %s\n", n.ID))
		sb.WriteString(fmt.Sprintf("时间: %s\n", timeStr))
		sb.WriteString(fmt.Sprintf("用户: %s (user_id: %s)", n.UserInfo.Nickname, n.UserInfo.UserID))
		if n.UserInfo.Indicator != "" {
			sb.WriteString(fmt.Sprintf("【%s】", n.UserInfo.Indicator))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("评论内容: %s\n", n.CommentInfo.Content))
		sb.WriteString(fmt.Sprintf("comment_id: %s\n", n.CommentInfo.ID))

		if n.Type == "comment/comment" && n.CommentInfo.TargetComment != nil {
			sb.WriteString(fmt.Sprintf("被回复的评论: [%s] %s\n",
				n.CommentInfo.TargetComment.UserInfo.Nickname,
				truncate(n.CommentInfo.TargetComment.Content, 60)))
			if n.ParentCommentID != "" {
				sb.WriteString(fmt.Sprintf("parent_comment_id: %s\n", n.ParentCommentID))
			}
		}

		sb.WriteString(fmt.Sprintf("笔记: %s\n", truncate(n.ItemInfo.Content, 40)))
		sb.WriteString(fmt.Sprintf("feed_id: %s\n", n.ItemInfo.ID))
		sb.WriteString(fmt.Sprintf("xsec_token: %s\n", n.ItemInfo.XsecToken))
		sb.WriteString("\n")
	}

	if result.HasMore {
		sb.WriteString(fmt.Sprintf("next_cursor=%s\n", result.NextCursor))
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: sb.String(),
		}},
	}
}

// handleGetUnprocessedNotifications 获取需要处理的通知（自动翻页+去重）
// 参数：
//   - processed_ids: 已完成的 notification_id 列表（JSON 数组字符串或逗号分隔）
//   - retry_ids: 待重试的 notification_id 列表
//   - max_pages: 最多扫描页数（默认3，全量补漏时传更大值）
//   - full_scan: true 时扫满 max_pages 页不提前停止（用于全量补漏扫描）
//   - since_hours: 只返回最近 N 小时内的通知（默认48），防止旧通知重复返回
//   - max_results: 单次最多返回多少条（默认20），防止输出截断
func (s *AppServer) handleGetUnprocessedNotifications(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	processedIDs := parseIDSet(args["processed_ids"])
	retryIDs := parseIDSet(args["retry_ids"])

	maxPagesFloat, _ := args["max_pages"].(float64)
	maxPages := int(maxPagesFloat)
	if maxPages <= 0 {
		maxPages = 3
	}

	stopAfterConsecutive := 5
	fullScan, _ := args["full_scan"].(bool)
	if fullScan {
		stopAfterConsecutive = 999999
	}

	// sinceUnix 从 processed_ids 和 retry_ids 里取最大的 notification_id（雪花 ID），
	// 提取其中的时间戳作为扫描起点——只返回这条最新已处理通知之后的通知。
	// 这样即使记录文件被清理，也不会因为旧 ID 消失而把大量旧通知误判为未处理。
	// 如果两个集合都为空（首次运行），退回到 since_hours 参数（默认 48 小时）。
	sinceHoursFloat, _ := args["since_hours"].(float64)
	sinceHours := int(sinceHoursFloat)
	if sinceHours <= 0 {
		sinceHours = 48
	}
	sinceUnix := extractSinceUnixFromIDs(processedIDs, retryIDs)
	if sinceUnix == 0 {
		// processed_ids 和 retry_ids 均为空（首次运行），退回到 since_hours 兜底
		sinceUnix = time.Now().Unix() - int64(sinceHours)*3600
	}

	maxResultsFloat, _ := args["max_results"].(float64)
	maxResults := int(maxResultsFloat)
	if maxResults <= 0 {
		maxResults = 20
	}

	logrus.Infof("MCP: 获取未处理通知 - processed=%d, retry=%d, maxPages=%d, fullScan=%v, sinceUnix=%d, maxResults=%d",
		len(processedIDs), len(retryIDs), maxPages, fullScan, sinceUnix, maxResults)

	result, err := s.xiaohongshuService.GetUnprocessedNotifications(ctx, processedIDs, retryIDs, maxPages, stopAfterConsecutive, sinceUnix, maxResults)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取通知失败: " + err.Error()}},
			IsError: true,
		}
	}

	// 找出 retry_ids 里在本次扫描中未出现的 ID（可能超出翻页范围或通知已消失）
	// 这些 ID 需要单独告知 Liko，让她决定是继续重试还是标记为已跳过
	seenIDs := make(map[string]bool)
	for _, n := range result.Notifications {
		seenIDs[n.NotificationID] = true
	}
	var missingRetryIDs []string
	for id := range retryIDs {
		if !seenIDs[id] {
			missingRetryIDs = append(missingRetryIDs, id)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("扫描完成：共扫描 %d 页 %d 条通知，跳过已完成 %d 条，过滤超时窗口 %d 条，待处理 %d 条（全新 %d + 待重试 %d）\n",
		result.PagesScanned, result.TotalScanned, result.TotalSkipped, result.TotalTooOld,
		result.TotalNew+result.TotalRetry, result.TotalNew, result.TotalRetry))
	if result.HasMore {
		sb.WriteString("⚠️ 待处理通知超过单次返回上限，本次仅返回部分。处理完后请再次调用获取剩余通知。\n")
	}
	if len(missingRetryIDs) > 0 {
		sb.WriteString(fmt.Sprintf("⚠️ 以下 %d 个待重试通知在本次扫描范围内未找到（可能已超出翻页范围或通知已消失），请酌情标记为已跳过：\n", len(missingRetryIDs)))
		for _, id := range missingRetryIDs {
			sb.WriteString(fmt.Sprintf("  - %s\n", id))
		}
	}
	sb.WriteString("\n")

	if len(result.Notifications) == 0 {
		sb.WriteString("✅ 没有需要处理的通知")
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: sb.String()}},
		}
	}

	for i, n := range result.Notifications {
		tag := "全新"
		if n.IsRetry {
			tag = "待重试"
		}

		var relationLabel string
		switch n.RelationType {
		case xiaohongshu.RelationCommentOnMyNote:
			relationLabel = "评论了我的笔记"
		case xiaohongshu.RelationReplyToMyComment:
			relationLabel = "回复了我的评论"
		case xiaohongshu.RelationAtOthersUnderMyComment:
			relationLabel = "在我的评论下@了他人"
		case xiaohongshu.RelationMentionedMe:
			relationLabel = "在评论中@了我"
		default:
			relationLabel = string(n.RelationType)
		}

		sb.WriteString(fmt.Sprintf("--- 待处理通知 %d [%s][%s] ---\n", i+1, tag, relationLabel))
		sb.WriteString(fmt.Sprintf("notification_id: %s\n", n.NotificationID))
		sb.WriteString(fmt.Sprintf("时间: %s\n", n.TimeCST))
		sb.WriteString(fmt.Sprintf("用户: %s (user_id: %s)\n", n.UserNickname, n.UserID))
		sb.WriteString(fmt.Sprintf("评论内容: %s\n", n.CommentContent))
		sb.WriteString(fmt.Sprintf("comment_id: %s\n", n.CommentID))

		if n.ParentCommentID != "" {
			sb.WriteString(fmt.Sprintf("parent_comment_id: %s\n", n.ParentCommentID))
		}
		if n.TargetCommentContent != "" {
			sb.WriteString(fmt.Sprintf("被回复的评论: [%s] %s\n",
				n.TargetCommentAuthor, truncate(n.TargetCommentContent, 60)))
		}

		sb.WriteString(fmt.Sprintf("笔记: %s\n", truncate(n.NoteTitle, 40)))
		sb.WriteString(fmt.Sprintf("feed_id: %s\n", n.FeedID))
		sb.WriteString(fmt.Sprintf("xsec_token: %s\n", n.XsecToken))
		sb.WriteString("\n")
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: sb.String()}},
	}
}

// extractSinceUnixFromIDs 从 processed_ids / retry_ids 中提取最小雪花 ID 对应的 Unix 时间戳（秒）。
// 小红书雪花 ID 高 41 位是毫秒时间戳（相对于 2013-01-01 00:00:00 UTC 的偏移）。
//
// 取最小 ID（最早的已处理通知）对应的时间作为扫描起点下界，确保所有已处理通知都在扫描窗口内，
// 从而保证去重逻辑能正确过滤掉已处理的通知，防止重复回复。
//
// 如果所有集合都为空（首次运行），返回 0，调用方退回 since_hours 兜底。
func extractSinceUnixFromIDs(sets ...map[string]bool) int64 {
	// 小红书雪花 ID epoch：2013-01-01 00:00:00 UTC（毫秒）
	const xhsEpochMs int64 = 1356998400000

	var minID uint64
	for _, set := range sets {
		for id := range set {
			var v uint64
			_, err := fmt.Sscanf(id, "%d", &v)
			if err != nil || v == 0 {
				continue
			}
			if minID == 0 || v < minID {
				minID = v
			}
		}
	}

	if minID == 0 {
		return 0
	}

	// 雪花 ID 右移 22 位得到相对 epoch 的毫秒偏移
	msOffset := int64(minID >> 22)
	unixMs := xhsEpochMs + msOffset
	// 额外往前多看 5 分钟，防止时钟误差或边界通知被漏掉
	return unixMs/1000 - 300
}

// parseIDSet 将参数解析为 notification_id 的 set
// 支持两种格式：
//   - []interface{}（JSON 数组）
//   - string（逗号分隔）
func parseIDSet(v interface{}) map[string]bool {
	result := make(map[string]bool)
	if v == nil {
		return result
	}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				result[s] = true
			}
		}
	case string:
		if val == "" {
			return result
		}
		for _, id := range strings.Split(val, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				result[id] = true
			}
		}
	}
	return result
}

// truncate 截断字符串到指定长度
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
