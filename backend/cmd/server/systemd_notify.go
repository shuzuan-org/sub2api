package main

import (
	"net"
	"os"
	"strings"
)

// sdNotify 通过 $NOTIFY_SOCKET 指向的 unixgram socket 向 systemd 发送服务状态通知（sd_notify 协议）。
// 当未运行在 Type=notify 单元下（NOTIFY_SOCKET 未设）时为 no-op，因此本地/测试运行不受影响。
//
// 在 tableflip 零停机升级中使用：
//   - "READY=1\nMAINPID=<pid>"：在 upg.Ready() 之后，（可能是 exec 出来的）新进程告知 systemd
//     自己已就绪并成为 main process。配合单元里的 NotifyAccess=all，systemd 会把 MAINPID 重定向到
//     新 pid，从而不把旧父进程的退出当作崩溃。
//   - "STOPPING=1"：仅在真正停机（SIGINT/SIGTERM）时发送，让 systemd 知道这是有意停机。
//     升级交接（SIGHUP）时绝不发送——新进程已接管 MAINPID，发送会让 systemd 进入 deactivating 并杀掉新进程。
//
// $NOTIFY_SOCKET 可能是抽象 socket（以 '@' 开头，映射为 NUL 字节）。
func sdNotify(state string) {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return
	}
	if strings.HasPrefix(socket, "@") {
		socket = "\x00" + socket[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}
