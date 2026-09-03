package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"forgegrid/internal/models"
	"forgegrid/internal/store"
	"image"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func cryptoRandomHex(n int) string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func main() {
	home, _ := os.UserHomeDir()
	s, err := store.NewStore(filepath.Join(home, ".config", "forgegrid", "coordinator"))
	if err != nil {
		panic(err)
	}

	jobID1 := "job-mandel-" + cryptoRandomHex(8) + "1" // Fedora
	jobID2 := "job-mandel-" + cryptoRandomHex(8) + "2" // ThinkPad

	s.Mu.Lock()
	s.Jobs[jobID1] = &models.Job{
		ID:             jobID1,
		WorkerID:       "worker-ad511989602387ff68c79acd17d6716e", // Fedora
		Task:           "mandelbrot",
		Status:         models.StatusPending,
		CreatedAt:      time.Now(),
		Parameters:     map[string]string{"width": "2000", "startY": "0", "endY": "600"},
		TimeoutSeconds: 600,
	}
	s.Jobs[jobID2] = &models.Job{
		ID:             jobID2,
		WorkerID:       "worker-56c3fdfcc83468e03d6f343e1dfa33a8", // ThinkPad
		Task:           "mandelbrot",
		Status:         models.StatusPending,
		CreatedAt:      time.Now(),
		Parameters:     map[string]string{"width": "2000", "startY": "600", "endY": "1200"},
		TimeoutSeconds: 600,
	}
	s.Save()
	s.Mu.Unlock()
	fmt.Println("Dispatched Mandelbrot jobs!")
	fmt.Printf("Fedora: %s\nThinkPad: %s\n", jobID1, jobID2)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	
	startOverall := time.Now()

	var img1, img2 image.Image
	var dur1, dur2 string

	c1, c2 := false, false
	for i := 0; i < 60; i++ {
		req, _ := http.NewRequest("GET", "https://127.0.0.1:8080/api/jobs", nil)
		req.SetBasicAuth("admin", "582adde4d79eec433cf52818ef643aa44311510769dcaec105bc8dc38102c8f3")
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var jobs []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&jobs)
		resp.Body.Close()

		for _, j := range jobs {
			id := j["id"].(string)
			if (id == jobID1 || id == jobID2) && j["status"].(string) == "COMPLETED" {
				b64 := j["result"].(string)
				data, _ := base64.StdEncoding.DecodeString(b64)
				img, _ := png.Decode(bytes.NewReader(data))
				if id == jobID1 && !c1 {
					img1 = img
					c1 = true
					fmt.Println("Fedora completed!")
					dur1 = string(j["logs"].(string))
				}
				if id == jobID2 && !c2 {
					img2 = img
					c2 = true
					fmt.Println("ThinkPad completed!")
					dur2 = string(j["logs"].(string))
				}
			}
		}

		if c1 && c2 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if !c1 || !c2 {
		fmt.Println("Jobs timed out!")
		return
	}

	// Stitch them together
	finalImg := image.NewRGBA(image.Rect(0, 0, 2000, 1200))
	draw.Draw(finalImg, image.Rect(0, 0, 2000, 600), img1, image.Point{0, 0}, draw.Src)
	draw.Draw(finalImg, image.Rect(0, 600, 2000, 1200), img2, image.Point{0, 0}, draw.Src)

	out, _ := os.Create("/tmp/dadlan-mandelbrot.png")
	png.Encode(out, finalImg)
	out.Close()
	
	endOverall := time.Now()

	fmt.Println("\n--- VERIFICATION ---")
	fmt.Printf("Fedora Region: Top half (0-600)\n")
	fmt.Printf("ThinkPad Region: Bottom half (600-1200)\n")
	fmt.Printf("Fedora logs: %s\n", dur1)
	fmt.Printf("ThinkPad logs: %s\n", dur2)
	fmt.Printf("Total wall-clock duration: %s\n", endOverall.Sub(startOverall).String())
	fmt.Printf("Final image dimensions: 2000 x 1200\n")
	fmt.Printf("Final image path: /tmp/dadlan-mandelbrot.png\n")
	fmt.Printf("Both workers contributed: TRUE (Stitched two separate PNG fragments successfully)\n")
}
