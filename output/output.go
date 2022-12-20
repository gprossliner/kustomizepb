package output

import "fmt"

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	purple = "\033[35m"
	cyan   = "\033[36m"
	gray   = "\033[37m"
	white  = "\033[97m"

	bold = "\033[1m"
)

func outF(col, s string, arg ...any) {
	fmt.Print(col)
	fmt.Printf(s, arg...)
	fmt.Println(reset)
}

func out(col, s string) {
	fmt.Print(col)
	fmt.Print(s)
	fmt.Println(reset)
}

func HeadingF(s string, arg ...any) {
	outF(cyan+bold, s, arg...)
}

func InfoF(s string, arg ...any) {
	outF(cyan, s, arg...)
}

func Error(s string) {
	out(red, s)
}
