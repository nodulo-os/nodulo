// Package sandbox runs generated Python in a short-lived, isolated Docker container.
package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultImage     = "nodulo-python-sandbox:latest"
	defaultTimeout   = 30 * time.Second
	maxScriptBytes   = 64 * 1024
	maxOutputBytes   = 8 * 1024 * 1024
	maxImageBytes    = 4 * 1024 * 1024
	imageMarkerStart = "<image>"
	imageMarkerEnd   = "</image>"
	// Allows the maximum captured console output plus a Base64 encoded PNG.
	maxExecutorReply = 14 << 20
)

type remoteRunRequest struct {
	Script string `json:"script"`
}

type remoteRunResponse struct {
	Output string `json:"output"`
	Image  string `json:"image_base64,omitempty"`
	Error  string `json:"error,omitempty"`
}

const pythonBootstrap = `import base64, os, sys
source=sys.stdin.read()
exec(compile(source, '<generated>', 'exec'), {'__name__':'__main__'})
path='/tmp/plot.png'
if os.path.isfile(path):
 data=open(path,'rb').read()
 if data[:8] != b'\x89PNG\r\n\x1a\n': raise ValueError('plot.png is not a valid PNG')
 print('\n<image>'+base64.b64encode(data).decode('ascii')+'</image>')
`

// RunPython sends generated code to the dedicated executor. The public API
// never receives Docker daemon access; local execution is reserved for the broker.
func RunPython(ctx context.Context, script string) (output string, imageBase64 string, err error) {
	if ctx == nil {
		return "", "", errors.New("context is required")
	}
	if strings.TrimSpace(script) == "" {
		return "", "", errors.New("Python script is empty")
	}
	if len(script) > maxScriptBytes {
		return "", "", fmt.Errorf("Python script exceeds %d bytes", maxScriptBytes)
	}
	baseURL := strings.TrimSpace(os.Getenv("PYTHON_EXECUTOR_URL"))
	if baseURL == "" {
		return "", "", errors.New("executor Python não configurado; defina PYTHON_EXECUTOR_URL")
	}
	token := strings.TrimSpace(os.Getenv("PYTHON_EXECUTOR_TOKEN"))
	if len(token) < 32 {
		return "", "", errors.New("executor Python não autenticado; configure PYTHON_EXECUTOR_TOKEN")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", errors.New("PYTHON_EXECUTOR_URL deve ser uma URL HTTP(S) válida, sem credenciais")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/run"
	requestBody, err := json.Marshal(remoteRunRequest{Script: script})
	if err != nil {
		return "", "", fmt.Errorf("encode Python executor request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(requestBody))
	if err != nil {
		return "", "", fmt.Errorf("create Python executor request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: defaultTimeout + 10*time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("não foi possível acessar o executor Python: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxExecutorReply+1))
	if err != nil {
		return "", "", fmt.Errorf("read Python executor response: %w", err)
	}
	if len(payload) > maxExecutorReply {
		return "", "", errors.New("resposta do executor Python excedeu o limite permitido")
	}
	var result remoteRunResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", "", fmt.Errorf("decode Python executor response (HTTP %d): %w", response.StatusCode, err)
	}
	if response.StatusCode != http.StatusOK {
		message := strings.TrimSpace(result.Error)
		if message == "" {
			message = fmt.Sprintf("executor Python respondeu HTTP %d", response.StatusCode)
		}
		return result.Output, "", errors.New(message)
	}
	if result.Image != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(result.Image)
		if decodeErr != nil || len(decoded) > maxImageBytes || len(decoded) < 8 || !bytes.Equal(decoded[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			return "", "", errors.New("executor Python retornou uma imagem PNG inválida")
		}
	}
	return result.Output, result.Image, nil
}

// RunPythonLocal executes code with no network, a read-only root, resource
// limits, and a wall-clock timeout. It is called only by the dedicated broker.
func RunPythonLocal(ctx context.Context, script string) (output string, imageBase64 string, err error) {
	if ctx == nil {
		return "", "", errors.New("context is required")
	}
	if strings.TrimSpace(script) == "" {
		return "", "", errors.New("Python script is empty")
	}
	if len(script) > maxScriptBytes {
		return "", "", fmt.Errorf("Python script exceeds %d bytes", maxScriptBytes)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return "", "", errors.New("Docker CLI is unavailable for the Python sandbox")
	}
	timeout := defaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", "", context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	image := strings.TrimSpace(os.Getenv("NODULO_PYTHON_SANDBOX_IMAGE"))
	if image == "" {
		image = defaultImage
	}
	if !validImageReference(image) {
		return "", "", errors.New("NODULO_PYTHON_SANDBOX_IMAGE is not a valid local image reference")
	}
	inspect := exec.CommandContext(runCtx, "docker", "image", "inspect", image)
	if inspectErr := inspect.Run(); inspectErr != nil {
		if runCtx.Err() != nil {
			return "", "", fmt.Errorf("Python sandbox timed out while checking its image: %w", runCtx.Err())
		}
		return "", "", fmt.Errorf("Python sandbox image %q is not available locally; build it before enabling execution", image)
	}
	var randomID [12]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return "", "", fmt.Errorf("create sandbox container identifier: %w", err)
	}
	containerName := "nodulo-python-" + hex.EncodeToString(randomID[:])
	args := []string{
		"run", "--rm", "-i", "--name=" + containerName, "--network=none", "--memory=256m", "--memory-swap=256m",
		"--cpus=0.5", "--pids-limit=64", "--read-only", "--user=65534:65534",
		"--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--ulimit=nofile=64:64",
		"--shm-size=32m", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m,mode=1777",
		"--env=HOME=/tmp", "--env=MPLCONFIGDIR=/tmp/matplotlib",
		image, "python", "-I", "-c", pythonBootstrap,
	}
	command := exec.CommandContext(runCtx, "docker", args...)
	command.Stdin = strings.NewReader(script)
	captured := &limitedBuffer{limit: maxOutputBytes}
	command.Stdout = captured
	command.Stderr = captured
	if runErr := command.Run(); runErr != nil {
		if runCtx.Err() != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cleanupCancel()
			cleanupOutput, cleanupErr := exec.CommandContext(cleanupCtx, "docker", "rm", "--force", containerName).CombinedOutput()
			if cleanupErr != nil && !strings.Contains(strings.ToLower(string(cleanupOutput)), "no such container") {
				return strings.TrimSpace(captured.String()), "", fmt.Errorf("Python sandbox timed out; container cleanup failed: %w (cleanup: %v)", runCtx.Err(), cleanupErr)
			}
			return strings.TrimSpace(captured.String()), "", fmt.Errorf("Python sandbox timed out or was cancelled: %w", runCtx.Err())
		}
		if captured.overflow {
			return "", "", fmt.Errorf("Python sandbox output exceeded %d bytes", maxOutputBytes)
		}
		return strings.TrimSpace(captured.String()), "", fmt.Errorf("Python sandbox failed: %w", runErr)
	}
	if captured.overflow {
		return "", "", fmt.Errorf("Python sandbox output exceeded %d bytes", maxOutputBytes)
	}
	output, imageBase64, err = parseOutput(captured.String())
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(output), imageBase64, nil
}

func validImageReference(value string) bool {
	if value == "" || len(value) > 255 {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._:/-", character)
		if !valid || index == 0 && !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return false
		}
	}
	return true
}

func parseOutput(raw string) (string, string, error) {
	start := strings.LastIndex(raw, imageMarkerStart)
	if start < 0 {
		return strings.TrimSpace(raw), "", nil
	}
	endOffset := strings.Index(raw[start+len(imageMarkerStart):], imageMarkerEnd)
	if endOffset < 0 {
		return "", "", errors.New("Python sandbox returned an incomplete image marker")
	}
	end := start + len(imageMarkerStart) + endOffset
	encoded := strings.TrimSpace(raw[start+len(imageMarkerStart) : end])
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", fmt.Errorf("Python sandbox returned invalid image data: %w", err)
	}
	if len(decoded) > maxImageBytes {
		return "", "", fmt.Errorf("generated PNG exceeds %d bytes", maxImageBytes)
	}
	if len(decoded) < 8 || !bytes.Equal(decoded[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return "", "", errors.New("Python sandbox output is not a valid PNG")
	}
	return strings.TrimSpace(raw[:start] + raw[end+len(imageMarkerEnd):]), encoded, nil
}

type limitedBuffer struct {
	bytes.Buffer
	mu       sync.Mutex
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	length := len(value)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return length, nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		value = value[:remaining]
	}
	_, err := buffer.Buffer.Write(value)
	return length, err
}

func (buffer *limitedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.String()
}
