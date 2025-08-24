# scan

使用 Go 與 [chromedp](https://github.com/chromedp/chromedp) 的網站檢測工具。

## 特色
- 從 CSV 檔案讀取 `host` 與 `port` 組合。
- 每個組合以新的 headless Chrome 分頁執行，完成後即關閉。
- 自動追蹤前端與後端的 redirect，並等待網路空閒（最後一個 request 結束後 500 ms 內無新 request）。
- 取得整頁 HTML、回應代碼、頁面標題，並檢查是否含有密碼欄位或命中 allowlist。
- 已處理過的項目不會重複執行，可支援中斷續跑。

## 使用方法
```bash
go run main.go --file input.csv --output output.csv \
  --concurrency 16 --timeout 5
```

### 引數
- `--file`：輸入 CSV 路徑，預設 `input.csv`
- `--output`：輸出 CSV 路徑，預設 `output.csv`
- `--concurrency`：同時活躍的 Chrome 分頁上限，預設 `16`
- `--timeout`：單個頁面最大等待秒數，預設 `5`

### Input 格式
輸入檔需為 CSV 且包含 `host`、`port` 欄位。

### Output 欄位
`host,port,response_code,html_header,has_login_keyword,is_matched,pass_test,error`

若 `has_login_keyword` 或 `is_matched` 為 `true`，則 `pass_test` 會為 `true`。

## 建置
需安裝 Go 1.21 以上版本與 Chrome/Chromium。

```bash
go mod tidy
go build
```

