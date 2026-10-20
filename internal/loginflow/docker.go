package loginflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const dockerContainer = "workbuddy2api"

func (r *Runner) restartDocker(ctx context.Context) {
	psCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	out, err := r.RunCommand(
		psCtx,
		"docker",
		[]string{"ps", "--format", "{{.Names}}"},
		nil,
	)
	cancel()
	if err != nil || !containsLine(out.Stdout, dockerContainer) {
		fmt.Fprintf(r.Out, "容器 %s 未运行，auth 文件已保存，下次启动自动加载\n", dockerContainer)
		return
	}
	fmt.Fprintf(r.Out, "重启 %s 加载新账号...\n", dockerContainer)
	restartCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	restartOut, restartErr := r.RunCommand(
		restartCtx,
		"docker",
		[]string{"restart", dockerContainer},
		nil,
	)
	cancel()
	if restartErr != nil {
		fmt.Fprintf(r.Out, "服务重启失败: %s\n", requestError(restartOut.Stderr, restartErr))
		return
	}
	r.Sleep(2 * time.Second)
	apiKey := r.configAPIKey()
	statusCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(statusCtx, http.MethodGet, r.StatusURL, nil)
	if err != nil {
		fmt.Fprintln(r.Out, "服务已重启，当前账号数: ?")
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		fmt.Fprintln(r.Out, "服务已重启，当前账号数: ?")
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintln(r.Out, "服务已重启，当前账号数: ?")
		return
	}
	var status struct {
		Accounts []json.RawMessage `json:"accounts"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || json.Unmarshal(raw, &status) != nil {
		fmt.Fprintln(r.Out, "服务已重启，当前账号数: ?")
		return
	}
	fmt.Fprintf(r.Out, "服务已重启，当前账号数: %d\n", len(status.Accounts))
}

func containsLine(raw []byte, wanted string) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == wanted {
			return true
		}
	}
	return false
}

func (r *Runner) configAPIKey() string {
	raw, err := os.ReadFile(r.ConfigPath)
	if err != nil {
		return "test_key"
	}
	var config struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "test_key"
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return "test_key"
	}
	return config.APIKey
}
