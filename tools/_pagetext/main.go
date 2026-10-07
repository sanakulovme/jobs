package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"faangjobs/internal/source"
)

func main() {
	u, _ := url.Parse(os.Args[1])
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	b, _ := io.ReadAll(resp.Body)
	t := source.PageText(string(b), u)
	fmt.Println(len(b), len(t))
	fmt.Println(t)
}
