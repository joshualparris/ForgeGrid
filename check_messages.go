package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
)

func main() {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("GET", "https://10.245.173.178:8080/api/messages", nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("Status:", resp.StatusCode)
	fmt.Println("Body:", string(body))
}
