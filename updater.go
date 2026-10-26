package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Текущая версия программы; при выпуске релиза тег на GitHub должен быть "v" + version
const version = "1.0.2"

const (
	releasesAPI = "https://api.github.com/repos/Sereja-Lebeda/SpaceDot/releases"
	assetName   = "SpaceDot.exe"
)

type release struct {
	Tag    string `json:"tag_name"`
	Notes  string `json:"body"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r *release) version() string {
	return strings.TrimPrefix(r.Tag, "v")
}

func (r *release) assetURL() string {
	for _, a := range r.Assets {
		if a.Name == assetName {
			return a.URL
		}
	}
	return ""
}

var errNoRelease = errors.New("релиз не найден")

// Клиент для запросов к API: ответы маленькие, общий таймаут уместен
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Клиент для скачивания exe: без общего таймаута (на медленном канале 25 МБ
// качаются долго), вместо него — таймаут на заголовки и на «зависание» потока
var downloadClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 30 * time.Second,
}}

// Если данные не приходят столько времени — считаем загрузку зависшей
const downloadStallTimeout = 60 * time.Second

func fetchRelease(url string) (*release, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил %s", resp.Status)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func latestRelease() (*release, error) {
	return fetchRelease(releasesAPI + "/latest")
}

func releaseByVersion(v string) (*release, error) {
	return fetchRelease(releasesAPI + "/tags/v" + v)
}

// newerVersion сравнивает версии вида 1.2.3: true, если a новее b
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na != nb {
			return na > nb
		}
	}
	return false
}

// checkUpdate возвращает релиз, если на GitHub есть версия новее текущей, иначе nil
func checkUpdate() (*release, error) {
	r, err := latestRelease()
	if errors.Is(err, errNoRelease) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !newerVersion(r.version(), version) {
		return nil, nil
	}
	if r.assetURL() == "" {
		return nil, errors.New("в релизе " + r.Tag + " нет файла " + assetName)
	}
	return r, nil
}

// installUpdate скачивает новый exe и подменяет им текущий.
// Работающий exe на Windows нельзя перезаписать, но можно переименовать —
// старый файл становится SpaceDot.exe.old и удаляется при следующем запуске.
// progress (может быть nil) вызывается по ходу загрузки; total = -1, если размер неизвестен.
func installUpdate(r *release, progress func(done, total int64)) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tmp := exe + ".download"
	old := exe + ".old"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", r.assetURL(), nil)
	if err != nil {
		return err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("загрузка: %s", resp.Status)
	}

	// Сторож: каждый прочитанный кусок продлевает таймер, тишина дольше лимита отменяет загрузку
	var stalled atomic.Bool
	watchdog := time.AfterFunc(downloadStallTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer watchdog.Stop()

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, &progressReader{
		r:     resp.Body,
		total: resp.ContentLength,
		onRead: func(done, total int64) {
			watchdog.Reset(downloadStallTimeout)
			if progress != nil {
				progress(done, total)
			}
		},
	})
	if stalled.Load() {
		err = errors.New("загрузка прервана: нет данных от GitHub больше минуты")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}

	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Rename(old, exe)
		os.Remove(tmp)
		return err
	}
	return nil
}

type progressReader struct {
	r      io.Reader
	done   int64
	total  int64
	onRead func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.onRead(p.done, p.total)
	}
	return n, err
}

// restart запускает новый exe; текущий процесс после этого должен завершиться
func restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command(exe).Start()
}

// cleanupOldExe удаляет файл, оставшийся от предыдущего обновления.
// Старый процесс может ещё завершаться, поэтому пробуем несколько раз.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	for i := 0; i < 20; i++ {
		err := os.Remove(old)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
