package main

import (
	"context"
	"os"

	"github.com/happycrud/golib/net/http/ui/daisyui"
)

func main() {
	daisyui.Tooltip(daisyui.Text("xx"), "woshitishi", "tooltip-info", "tooltip-right").Render(context.Background(), os.Stdout)
}
