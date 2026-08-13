package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
)

func main() {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("POST", "https://10.245.173.178:8080/api/workers/message", nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Status:", resp.StatusCode)
}
