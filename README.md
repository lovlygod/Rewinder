# <img src="internal/public/icon.ico" width="32"> Rewinder — App Time Machine for Windows

**Automatic tracking and recovery of Windows application states**

## 🚀 Features

- **Automatic snapshots** - tracks window, file, and clipboard changes
- **Quick restoration** - restores application states with one click
- **Hotkeys** - `Ctrl + Alt + Z` for quick access to recovery
- **System tray** - runs in the background without interfering with work
- **Smart notifications** - compact notifications in the corner of the screen

## 📥 Installation

1. **Download** `Rewinder.exe` from [releases](../../releases)
2. **Run as administrator** (required for system monitoring)
3. **Use** `Ctrl + Alt + Z` to restore states
4. **Configure** through the system tray icon

## 🎮 Usage

### Main functions:
- **Timeline** - view all snapshots by applications
- **Quick Restore** - quick restoration via `Ctrl + Alt + Z`
- **Tray Menu** - control through system tray

### Hotkeys:
- `Ctrl + Alt + Z` - open restoration overlay
- `←/→` or scroll - navigate through snapshots
- `Enter` - restore selected snapshot
- `Esc` - close overlay

## ⚙️ System Requirements

- **Windows 10/11** (64-bit)
- **Administrator rights** (for monitoring system events)
- **WebView2** (usually already installed in Windows)

## 🔧 Technical Details

- **Backend**: Go with Windows system APIs
- **Frontend**: Svelte + TypeScript
- **Framework**: Wails v2
- **Architecture**: Event-driven with delta-based snapshots

## 🛡️ Privacy

- Stores only **hashes** of clipboard content (not the actual text)
- All data remains **locally** on your computer
- Does not send data to the internet

## 📝 License

MIT License - free use and modification

## 🐛 Report a Bug

If you found a bug or have suggestions - create an [Issue](../../issues)

---

**Author**: lovly 
**Version**: 0.1.0
