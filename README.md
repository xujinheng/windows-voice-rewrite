[![VoiceRewrite 30 秒操作演示](docs/demo/voicerewrite-preview.webp)](docs/demo/voicerewrite-demo.mp4)

[▶ 点击观看 30 秒演示视频](docs/demo/voicerewrite-demo.mp4)

# VoiceRewrite

**按住鼠标侧键说话，松开就能输入文字。** 支持 Windows；macOS 版本仍在测试。

<a href="README.en.md"><img alt="Switch to English" src="https://img.shields.io/badge/English-Switch_language-2563eb?style=for-the-badge"></a>

## 🚀 开始使用

1. **下载主程序。** Windows 下载 `VoiceRewrite-Windows.exe`，双击运行，不需要安装。M 系列 Mac 下载 `VoiceRewrite-Mac.dmg`，将应用拖到「应用程序」；Intel Mac 选择 `VoiceRewrite-Mac-Intel.dmg`。
2. **部署语音模型。** 打开 VoiceRewrite「设置」→「语音识别」，选择下载目录（Windows 默认在 EXE 旁边；Mac 安装在「应用程序」时默认为个人主目录），点击 **Deploy**。它会自动下载 Qwen3-ASR-1.7B 和对应的 llama.cpp CUDA/CPU/Metal 运行时，不需要单独安装 OneClick、Python 或 Homebrew。首次需要联网和数 GB 空间。
3. **开始听写。** Windows 默认按住鼠标下侧键（Mouse4）说话，松开后输入；Mac 默认按住右 Command。快捷键可在设置中修改。

安装的数据全部在 **`你选择的目录/Qwen3-ASR/`**，包含模型、运行时、下载缓存和服务日志。关闭 VoiceRewrite 后删除这个文件夹即可清理模型环境。已下载文件会复用，重启主程序自动恢复本地 ASR 服务。

**下载入口：** [GitHub Releases](https://github.com/xujinheng/windows-voice-rewrite/releases) · [最新 main 构建（Actions）](https://github.com/xujinheng/windows-voice-rewrite/actions/workflows/build.yml?query=branch%3Amain)

**版本说明：** v0.1.1 是历史版本，仍需要分别下载旧 OneClick 与 Windows 安装包。集成 Deploy 的单 EXE / Mac DMG 将在本次改动合并后的新构建中提供，尚未属于 v0.1.1 下载内容。

## ✨ AI 改写、翻译与解释

听写只需要上述内置 ASR。使用 AI 改写还需要文字模型，例如 [LM Studio](https://lmstudio.ai/)；在 LM Studio 加载聊天模型并开启本地 API（通常为 `http://127.0.0.1:1234/v1`），然后在 VoiceRewrite 设置里检查连接。Windows 默认使用 Mouse5 发出语音指令，Mac 默认使用右 Option。

## 🍎 macOS 注意事项

macOS 的听写和跨应用文本插入仍处于实验阶段。未经 Apple Developer ID 签名、公证的测试 DMG 可能被系统拦截；请核实来源并了解系统权限提示。需要按实际功能授予麦克风、辅助功能和输入监控权限。

[进阶说明](clients/windows/README.md) · [Mac 设置](clients/macos/app/README.md) · [隐私说明](PRIVACY.md) · [报告问题](https://github.com/xujinheng/windows-voice-rewrite/issues)

语音和选中文字只会发送到你配置的本地或远程模型接口。项目采用 [BSD 3-Clause 许可证](LICENSE)。
