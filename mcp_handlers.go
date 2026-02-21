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
//   - processed_ids: 已彻底完成的 notification_id 列表（已回复/已跳过），这些通知会被跳过
//   - retry_ids: 上次超时/报错待重试的 notification_id 列表（retry_reason=timeout）
//   - deleted_ids: 上次标记为已删除需重新确认的 notification_id 列表（retry_reason=deleted_recheck）
//   - max_pages: 最多扫描页数（默认3，全量补漏时传更大值）
//   - full_scan: true 时扫满 max_pages 页不提前停止（用于全量补漏扫描）
//   - since_hours: 兜底时间窗口（默认48小时），仅当所有 ID 列表均为空时生效
//   - max_results: 单次最多返回多少条（默认20），防止输出截断
// extractSinceUnixFromIDs 从 processed_ids 中提取最小雪花 ID 对应的 Unix 时间戳（秒）。
// 小红书雪花 ID 高 41 位是毫秒时间戳（相对于 2013-01-01 00:00:00 UTC 的偏移）。
//
// 取最小 ID（最早的已处理通知）对应的时间作为扫描起点下界，确保所有已处理通知都在扫描窗口内，
// 从而保证去重逻辑能正确过滤掉已处理的通知，防止重复回复。
//
// 注意：只传入 processedIDs，不传 retryIDs/deletedIDs——后两者不受时间窗口限制，
// 纳入计算只会把窗口拉得更早，增加不必要的扫描量。
//
// 如果 processedIDs 为空（首次运行），返回 0，调用方退回 since_hours 兜底。
func extractSinceUnixFromIDs(sets ...map[string]bool) int64 {
	// 小红书 notification_id 高位直接编码 Unix 秒时间戳：
	// notification_id >> 32 = Unix 时间戳（秒）
	// 注意：这与标准雪花 ID（>>22 + epoch）不同，小红书 notification_id 是自有格式。

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

	// 右移 32 位得到 Unix 秒时间戳，额外往前多看 5 分钟防止边界漏掉
	unixSec := int64(minID >> 32)
	return unixSec - 300
}

// truncate 截断字符串到指定长度
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// handleNotificationsGetPending 获取待处理通知列表（从 DB + 实时扫描合并）
func (s *AppServer) handleNotificationsGetPending(ctx context.Context, args NotificationsGetPendingArgs) *MCPToolResult {
	store, err := GetNotificationStore()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "初始化状态数据库失败: " + err.Error()}},
			IsError: true,
		}
	}

	// 自动跳过重试次数过多的通知
	skipped, err := store.AutoSkipExcessiveRetries(5)
	if err != nil {
		logrus.Warnf("AutoSkipExcessiveRetries 失败: %v", err)
	} else if skipped > 0 {
		logrus.Infof("自动跳过 %d 条重试次数超限的通知", skipped)
	}

	// 从 DB 读取各状态 ID 集合
	processedIDs, err := store.GetProcessedIDs()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "读取已处理 ID 失败: " + err.Error()}},
			IsError: true,
		}
	}
	retryIDs, err := store.GetRetryIDs()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "读取重试 ID 失败: " + err.Error()}},
			IsError: true,
		}
	}
	deletedCheckIDs, err := store.GetDeletedCheckIDs()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "读取待确认 ID 失败: " + err.Error()}},
			IsError: true,
		}
	}

	// 计算扫描起点：从 processedIDs 中最小雪花 ID 推算，或退回 since_hours
	sinceHours := args.SinceHours
	if sinceHours <= 0 {
		sinceHours = 48
	}
	maxPages := args.MaxPages
	if maxPages <= 0 {
		maxPages = 5
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	stopAfterConsecutive := 5
	if args.FullScan {
		stopAfterConsecutive = 999999
	}

	sinceUnix := extractSinceUnixFromIDs(processedIDs)
	if sinceUnix == 0 {
		// 首次运行或 DB 为空，用 last_fetch_time 兜底
		lastFetch, _ := store.GetLastFetchTime()
		if lastFetch > 0 {
			sinceUnix = lastFetch - 300
		} else {
			sinceUnix = time.Now().Unix() - int64(sinceHours)*3600
		}
	}

	logrus.Infof("notifications.get_pending: processed=%d, retry=%d, deleted_check=%d, maxPages=%d, sinceUnix=%d",
		len(processedIDs), len(retryIDs), len(deletedCheckIDs), maxPages, sinceUnix)

	// 调用底层扫描
	result, err := s.xiaohongshuService.GetUnprocessedNotifications(
		ctx, processedIDs, retryIDs, deletedCheckIDs,
		maxPages, stopAfterConsecutive, sinceUnix, maxResults,
	)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "扫描通知失败: " + err.Error()}},
			IsError: true,
		}
	}

	// 将扫描到的全新通知写入 DB（INSERT OR IGNORE，不覆盖已有状态）
	var newRecords []NotificationRecord
	for _, n := range result.Notifications {
		if n.RetryReason == xiaohongshu.RetryReasonNone {
			newRecords = append(newRecords, NotificationRecord{
				ID:              n.NotificationID,
				FeedID:          n.FeedID,
				XsecToken:       n.XsecToken,
				CommentID:       n.CommentID,
				ParentCommentID: n.ParentCommentID,
				CommentContent:  n.CommentContent,
				UserID:          n.UserID,
				UserNickname:    n.UserNickname,
				NoteTitle:       n.NoteTitle,
				RelationType:    string(n.RelationType),
				NotifTimeUnix:   n.TimeUnix,
			})
		}
	}
	if len(newRecords) > 0 {
		if err := store.UpsertNotifications(newRecords); err != nil {
			logrus.Warnf("写入新通知到 DB 失败: %v", err)
		}
	}

	// 更新 last_fetch_time 为本次扫描到的最新通知时间
	if len(result.Notifications) > 0 {
		latestTime := result.Notifications[0].TimeUnix
		if latestTime > 0 {
			_ = store.SetLastFetchTime(latestTime)
		}
	}

	// 从 DB 读取所有待处理记录（pending/retry/deleted_check），
	// 与扫描结果合并——确保即使扫描页数不足，DB 里的旧 pending 也不会丢失。
	dbPendingRecords, err := store.GetPendingRecords()
	if err != nil {
		logrus.Warnf("读取 DB pending 记录失败: %v", err)
	}

	// 以扫描结果为基础，补充 DB 里有但扫描未覆盖到的 pending 记录
	scannedIDs := make(map[string]bool)
	for _, n := range result.Notifications {
		scannedIDs[n.NotificationID] = true
	}

	// 构建最终输出列表：先放扫描结果，再追加 DB 里未被扫描覆盖的旧 pending
	type outputEntry struct {
		fromScan bool
		scan     xiaohongshu.UnprocessedNotification
		db       NotificationRecord
	}
	var entries []outputEntry
	for _, n := range result.Notifications {
		entries = append(entries, outputEntry{fromScan: true, scan: n})
	}
	var dbOnlyCount int
	for _, r := range dbPendingRecords {
		if !scannedIDs[r.ID] {
			entries = append(entries, outputEntry{fromScan: false, db: r})
			dbOnlyCount++
		}
	}

	logrus.Infof("notifications.get_pending: 扫描返回 %d 条，DB 补充 %d 条旧 pending，合计 %d 条",
		len(result.Notifications), dbOnlyCount, len(entries))

	// 构建输出
	var sb strings.Builder
	total := result.TotalNew + result.TotalRetry + result.TotalDeletedRecheck
	sb.WriteString(fmt.Sprintf("扫描完成：%d 页 %d 条，跳过已完成 %d 条，扫描待处理 %d 条（全新 %d + 重试 %d + 删除重确认 %d）",
		result.PagesScanned, result.TotalScanned, result.TotalSkipped,
		total, result.TotalNew, result.TotalRetry, result.TotalDeletedRecheck))
	if dbOnlyCount > 0 {
		sb.WriteString(fmt.Sprintf("，DB补充旧pending %d 条", dbOnlyCount))
	}
	sb.WriteString("\n")

	if result.HasMore {
		sb.WriteString("⚠️ 扫描到的待处理通知超过单次返回上限，处理完后请再次调用。\n")
	}
	sb.WriteString("\n")

	if len(entries) == 0 {
		sb.WriteString("✅ 没有需要处理的通知。")
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: sb.String()}},
		}
	}

	for i, e := range entries {
		if e.fromScan {
			n := e.scan
			var tag string
			switch n.RetryReason {
			case xiaohongshu.RetryReasonTimeout:
				tag = "重试"
			case xiaohongshu.RetryReasonDeletedRecheck:
				tag = "删除重确认"
			default:
				tag = "全新"
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
			sb.WriteString(fmt.Sprintf("--- 通知 %d [%s][%s] ---\n", i+1, tag, relationLabel))
			sb.WriteString(fmt.Sprintf("notification_id: %s\n", n.NotificationID))
			sb.WriteString(fmt.Sprintf("时间: %s\n", n.TimeCST))
			sb.WriteString(fmt.Sprintf("用户: %s (user_id: %s)\n", n.UserNickname, n.UserID))
			sb.WriteString(fmt.Sprintf("评论: %s\n", n.CommentContent))
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
		} else {
			r := e.db
			var tag string
			switch r.Status {
			case StatusRetry:
				tag = "重试"
			case StatusDeletedCheck:
				tag = "删除重确认"
			default:
				tag = "pending"
			}
			timeCST := time.Unix(r.NotifTimeUnix, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
			sb.WriteString(fmt.Sprintf("--- 通知 %d [%s][DB补充][%s] ---\n", i+1, tag, r.RelationType))
			sb.WriteString(fmt.Sprintf("notification_id: %s\n", r.ID))
			sb.WriteString(fmt.Sprintf("时间: %s\n", timeCST))
			sb.WriteString(fmt.Sprintf("用户: %s (user_id: %s)\n", r.UserNickname, r.UserID))
			sb.WriteString(fmt.Sprintf("评论: %s\n", r.CommentContent))
			sb.WriteString(fmt.Sprintf("comment_id: %s\n", r.CommentID))
			if r.ParentCommentID != "" {
				sb.WriteString(fmt.Sprintf("parent_comment_id: %s\n", r.ParentCommentID))
			}
			sb.WriteString(fmt.Sprintf("笔记: %s\n", truncate(r.NoteTitle, 40)))
			sb.WriteString(fmt.Sprintf("feed_id: %s\n", r.FeedID))
			sb.WriteString(fmt.Sprintf("xsec_token: %s\n", r.XsecToken))
		}
		sb.WriteString("\n")
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: sb.String()}},
	}
}

// handleNotificationsMarkResult 标记通知处理结果
func (s *AppServer) handleNotificationsMarkResult(ctx context.Context, args NotificationsMarkResultArgs) *MCPToolResult {
	if args.NotificationID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "缺少 notification_id"}},
			IsError: true,
		}
	}

	store, err := GetNotificationStore()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "初始化状态数据库失败: " + err.Error()}},
			IsError: true,
		}
	}

	var status NotificationStatus
	switch args.Status {
	case "replied":
		status = StatusReplied
	case "skipped":
		status = StatusSkipped
	case "retry":
		status = StatusRetry
	case "deleted_check":
		status = StatusDeletedCheck
	default:
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("无效的 status: %q，合法值：replied / skipped / retry / deleted_check", args.Status)}},
			IsError: true,
		}
	}

	if err := store.MarkResult(args.NotificationID, status, args.ReplyContent); err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "更新状态失败: " + err.Error()}},
			IsError: true,
		}
	}

	logrus.Infof("notifications.mark_result: id=%s status=%s", args.NotificationID, status)

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("✅ 通知 %s 已标记为 %s", args.NotificationID, status)}},
	}
}

// handleNotificationsStats 返回通知状态统计
func (s *AppServer) handleNotificationsStats(ctx context.Context) *MCPToolResult {
	store, err := GetNotificationStore()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "初始化状态数据库失败: " + err.Error()}},
			IsError: true,
		}
	}

	stats, err := store.Stats()
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "读取统计失败: " + err.Error()}},
			IsError: true,
		}
	}

	lastFetch, _ := store.GetLastFetchTime()
	var lastFetchStr string
	if lastFetch > 0 {
		lastFetchStr = time.Unix(lastFetch, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	} else {
		lastFetchStr = "从未"
	}

	var sb strings.Builder
	sb.WriteString("通知状态统计：\n")
	sb.WriteString(fmt.Sprintf("  待处理 (pending):      %d\n", stats["pending"]))
	sb.WriteString(fmt.Sprintf("  待重试 (retry):        %d\n", stats["retry"]))
	sb.WriteString(fmt.Sprintf("  删除待确认 (deleted_check): %d\n", stats["deleted_check"]))
	sb.WriteString(fmt.Sprintf("  已回复 (replied):      %d\n", stats["replied"]))
	sb.WriteString(fmt.Sprintf("  已跳过 (skipped):      %d\n", stats["skipped"]))
	sb.WriteString(fmt.Sprintf("上次拉取时间: %s\n", lastFetchStr))

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: sb.String()}},
	}
}
