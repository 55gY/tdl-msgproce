// tdl-msgproce - Bot 菜单与按钮
//
// - 命令菜单：通过 Bot API setMyCommands 注册，客户端输入 / 时展示官方命令列表
// - 常驻回复键盘：输入框上方的按钮，一点即发，按钮文本映射到已有命令
//
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
package main

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// menuButtonCommands 常驻键盘按钮文本 -> 对应命令
// 新增命令时同步维护此表、下方键盘布局与 setMyCommands 清单
var menuButtonCommands = map[string]string{
	"📦 备份配置": "/backup",
	"📊 运行状态": "/status",
	"📖 使用帮助": "/help",
	"👋 开始使用": "/start",
}

// botMenuCommands setMyCommands 注册的命令清单
var botMenuCommands = []tgbotapi.BotCommand{
	{Command: "start", Description: "开始使用"},
	{Command: "help", Description: "使用帮助"},
	{Command: "status", Description: "查看运行状态"},
	{Command: "backup", Description: "备份 tdl 配置文件"},
}

// registerBotCommands 注册 Bot 命令菜单（客户端 / 列表），失败不阻塞运行
func (p *MessageProcessor) registerBotCommands(bot *tgbotapi.BotAPI) {
	if _, err := bot.Request(tgbotapi.NewSetMyCommands(botMenuCommands...)); err != nil {
		fmt.Printf("⚠️  注册 Bot 命令菜单失败: %v\n", err)
		return
	}
	fmt.Printf("✅ Bot 命令菜单已注册 (%d 个命令)\n", len(botMenuCommands))
}

// mainMenuKeyboard 常驻回复键盘：2x2，覆盖全部现有指令
func mainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📦 备份配置"),
			tgbotapi.NewKeyboardButton("📊 运行状态"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📖 使用帮助"),
			tgbotapi.NewKeyboardButton("👋 开始使用"),
		),
	)
	keyboard.ResizeKeyboard = true
	return keyboard
}

// sendBotReplyWithKeyboard 发送带常驻键盘的回复消息
func (p *MessageProcessor) sendBotReplyWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, replyToID int, text string, keyboard tgbotapi.ReplyKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyToID
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// resolveMenuButton 常驻键盘按钮文本 -> 命令，未命中返回原文本
func resolveMenuButton(text string) string {
	if cmd, ok := menuButtonCommands[text]; ok {
		return cmd
	}
	return text
}
