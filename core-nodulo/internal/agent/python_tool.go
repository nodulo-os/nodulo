package agent

import (
	"context"
	"strings"

	"core-nodulo/pkg/sandbox"
)

// ExecutePythonTool runs a model-authored script in the restricted Docker
// sandbox. The image is returned separately so Base64 does not bloat the
// model's observation/context window.
type ExecutePythonTool struct{}

func (ExecutePythonTool) Name() string { return "execute_python" }

func (ExecutePythonTool) Execute(ctx context.Context, script string) (ToolObservation, error) {
	output, image, err := sandbox.RunPython(ctx, normalizePythonScript(script))
	if err != nil {
		return ToolObservation{Text: strings.TrimSpace(output)}, err
	}
	if strings.TrimSpace(output) == "" {
		output = "O código foi executado sem produzir texto no console."
	}
	if image != "" {
		output += "\nGráfico PNG gerado com sucesso e anexado à resposta."
	}
	return ToolObservation{Text: output, ImageBase64: image}, nil
}

func normalizePythonScript(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "```") {
		firstNewline := strings.IndexByte(input, '\n')
		if firstNewline >= 0 {
			input = input[firstNewline+1:]
		}
		if closingFence := strings.LastIndex(input, "```"); closingFence >= 0 {
			input = input[:closingFence]
		}
	}
	if strings.TrimSpace(input) == "" {
		return ""
	}
	return input
}

