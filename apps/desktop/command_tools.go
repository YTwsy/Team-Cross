package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"teamcross/apps/desktop/internal/commandtools"
	"teamcross/internal/cliinstall"
	"teamcross/internal/problem"
	"teamcross/internal/uilanguage"
)

func (s *desktopShell) commandLineTools() {
	if s.busy() || !s.operationBusy.CompareAndSwap(false, true) {
		return
	}
	defer s.operationBusy.Store(false)
	text := func(zh, en string) string {
		if localLanguage(s.directory) == uilanguage.Chinese {
			return zh
		}
		return en
	}
	application.InvokeSync(func() { s.showMain("") })
	notice := func(message string) {
		closed := make(chan struct{})
		dialog := s.app.Dialog.Info().SetTitle("Team Cross").SetMessage(message).AttachToWindow(s.main)
		dialog.AddButton(text("关闭", "Close")).SetAsDefault().SetAsCancel().OnClick(func() { close(closed) })
		dialog.Show()
		<-closed
	}
	executable, err := os.Executable()
	if err != nil {
		notice(text("无法找到 App 内的命令行工具。", "The bundled command line tool could not be found."))
		return
	}
	directory := os.Getenv("TEAMCROSS_CLI_DIR")
	if directory == "" {
		directory = cliinstall.DefaultDir
	}
	if !filepath.IsAbs(directory) {
		notice(text("命令目录必须是绝对路径。", "The command directory must be an absolute path."))
		return
	}
	manager := commandtools.Manager{Helper: filepath.Join(filepath.Dir(executable), "..", "Resources", "teamcross"), Directory: directory, SearchPath: os.Getenv("PATH")}
	status := manager.Status()
	command := status.Command
	if command == "" {
		command = text("尚未安装", "Not installed")
	}
	message := fmt.Sprintf(text("当前命令：%s\n安装位置：%s\n\n安装后可在终端运行 teamcross。移除命令入口不会停止服务或删除协作数据。", "Current command: %s\nInstall location: %s\n\nRun teamcross in Terminal after installation. Removing the command does not stop Core or delete collaboration data."), command, status.Target)
	if status.Conflict != "" {
		message += fmt.Sprintf(text("\n\n已有命令由其他安装管理：%s。请通过原安装渠道切换。", "\n\nAnother installation owns %s. Switch using its original installation channel."), status.Conflict)
	} else if !status.CanInstall && !status.CanRemove {
		message += text("\n\n现有命令由安装渠道管理，无需重复安装。", "\n\nYour installation channel manages this command; no additional launcher is needed.")
	}
	choice := make(chan string, 1)
	dialog := s.app.Dialog.Question().SetTitle(text("Team Cross 命令行工具", "Team Cross Command Line Tools")).SetMessage(message).AttachToWindow(s.main)
	if status.CanInstall && !status.Installed {
		dialog.AddButton(text("安装命令", "Install Command")).OnClick(func() { choice <- "install" })
	}
	if status.CanRemove {
		dialog.AddButton(text("移除命令", "Remove Command")).OnClick(func() { choice <- "remove" })
	}
	dialog.AddButton(text("关闭", "Close")).SetAsDefault().SetAsCancel().OnClick(func() { choice <- "close" })
	dialog.Show()
	operation := <-choice
	if operation == "close" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status, err = manager.Change(ctx, operation == "remove", false)
	if err != nil && problem.Describe(err).Code == "cli_permission_denied" {
		// The system asks for authorization only for this fixed helper command.
		// Core and the desktop host keep the current user's privileges.
		status, err = manager.Change(ctx, operation == "remove", true)
	}
	if err != nil {
		notice(text("命令入口更改尚未确认。请重新打开“命令行工具”查看当前状态后再决定是否重试。", "The command change was not confirmed. Reopen Command Line Tools to check its current status before retrying."))
		return
	}
	message = text("命令入口已移除。App、服务和协作数据继续保留。", "The command was removed. The App, Core and collaboration data are preserved.")
	if operation == "install" {
		message = text("命令行工具已安装。打开终端，运行 teamcross。\n", "The command line tool is installed. Open Terminal and run teamcross.\n") + status.Target
		if !status.PathReady {
			message += text("\n如终端找不到命令，请检查 PATH 是否包含该目录。", "\nIf Terminal cannot find the command, check that PATH includes this directory.")
		}
	}
	notice(message)
}
