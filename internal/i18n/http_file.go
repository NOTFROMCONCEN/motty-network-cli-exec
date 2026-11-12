package i18n

// HTTPFileMessages are the texts of the HTTP file-server page: the directory
// that is shared, the switches a visitor's rights hang on, and the live state
// of the listener once it is up.
type HTTPFileMessages struct {
	Title       string
	Description string

	// The listener's state and what it answers on.
	StatusRunning string
	StatusStopped string
	AddressLabel  string
	CopyAddress   string
	AddressCopied string
	OpenAddress   string
	OpenFailedFmt string
	ExternalHint  string
	SinceFmt      string

	// The counters a run reports.
	StatsRequests   string
	StatsUploaded   string
	StatsDownloaded string
	StatsActive     string
	RequestsFmt     string
	UploadedFmt     string
	DownloadedFmt   string
	ActiveFmt       string

	// The settings: what is shared, where, and what a visitor may do.
	SettingsTitle        string
	DirectoryLabel       string
	DirectoryPlaceholder string
	Browse               string
	BrowseFailedFmt      string
	PortLabel            string
	PortPlaceholder      string
	PortHint             string
	LockedHint           string
	AllowUpload          string
	AllowUploadHint      string
	AllowDelete          string
	AllowDeleteHint      string
	AllowListing         string
	AllowListingHint     string

	// The actions and what they report.
	Start          string
	Stop           string
	StartedFmt     string
	Stopped        string
	StartFailedFmt string
	StopFailedFmt  string
	InvalidPort    string
	InvalidRoot    string
}

var chineseHTTPFile = HTTPFileMessages{
	Title:       "HTTP 文件服务器",
	Description: "在本机创建 HTTP 文件服务器，供内外网机器连接后上传、下载文件。服务期间请勿关闭本页面所在的 Motty。",

	StatusRunning: "运行中",
	StatusStopped: "已停止",
	AddressLabel:  "访问地址",
	CopyAddress:   "复制地址",
	AddressCopied: "地址已复制到剪贴板",
	OpenAddress:   "在浏览器中打开",
	OpenFailedFmt: "无法打开浏览器：%s",
	ExternalHint:  "同一网络内的机器可直接访问上述地址；在路由器上放通端口后，外网同样可以访问。",
	SinceFmt:      "启动于 %s",

	StatsRequests:   "请求",
	StatsUploaded:   "已上传",
	StatsDownloaded: "已下载",
	StatsActive:     "进行中",
	RequestsFmt:     "%d 次请求",
	UploadedFmt:     "%s",
	DownloadedFmt:   "%s",
	ActiveFmt:       "%d 个进行中",

	SettingsTitle:        "服务器设置",
	DirectoryLabel:       "共享目录",
	DirectoryPlaceholder: "选择或输入要共享的目录",
	Browse:               "浏览…",
	BrowseFailedFmt:      "无法打开目录选择器：%s",
	PortLabel:            "端口",
	PortPlaceholder:      "8080",
	PortHint:             "1–65535；其他机器通过 本机IP:端口 访问",
	LockedHint:           "服务器运行中，共享目录和端口需要停止后才能修改",
	AllowUpload:          "允许上传",
	AllowUploadHint:      "其他机器可以用 PUT、POST 或网页表单上传文件到共享目录",
	AllowDelete:          "允许删除",
	AllowDeleteHint:      "其他机器可以用 DELETE 删除共享目录中的文件",
	AllowListing:         "目录列表",
	AllowListingHint:     "浏览器访问目录时显示文件列表，允许上传时附带上传表单",

	Start:          "启动服务器",
	Stop:           "停止服务器",
	StartedFmt:     "服务器已启动：%s",
	Stopped:        "服务器已停止",
	StartFailedFmt: "启动失败：%s",
	StopFailedFmt:  "停止失败：%s",
	InvalidPort:    "端口必须是 1 到 65535 之间的数字",
	InvalidRoot:    "共享目录不存在，或不是一个目录",
}

var englishHTTPFile = HTTPFileMessages{
	Title:       "HTTP File Server",
	Description: "Serve one folder of this machine over HTTP so other machines can download from it and upload to it. Keep Motty open while the server runs.",

	StatusRunning: "Running",
	StatusStopped: "Stopped",
	AddressLabel:  "Address",
	CopyAddress:   "Copy address",
	AddressCopied: "Address copied to the clipboard",
	OpenAddress:   "Open in browser",
	OpenFailedFmt: "Cannot open the browser: %s",
	ExternalHint:  "Machines on the same network can use the address above as it is; forward the port on the router and the internet can reach it too.",
	SinceFmt:      "Started at %s",

	StatsRequests:   "Requests",
	StatsUploaded:   "Uploaded",
	StatsDownloaded: "Downloaded",
	StatsActive:     "Active",
	RequestsFmt:     "%d requests",
	UploadedFmt:     "%s",
	DownloadedFmt:   "%s",
	ActiveFmt:       "%d active",

	SettingsTitle:        "Server settings",
	DirectoryLabel:       "Shared folder",
	DirectoryPlaceholder: "Choose or type the folder to share",
	Browse:               "Browse…",
	BrowseFailedFmt:      "Cannot open the folder picker: %s",
	PortLabel:            "Port",
	PortPlaceholder:      "8080",
	PortHint:             "1–65535; other machines reach it at this-machine-ip:port",
	LockedHint:           "The server is running: stop it to change the shared folder or the port",
	AllowUpload:          "Allow uploads",
	AllowUploadHint:      "Other machines may write files with PUT, POST or the web form",
	AllowDelete:          "Allow deletes",
	AllowDeleteHint:      "Other machines may remove files with DELETE",
	AllowListing:         "Directory listing",
	AllowListingHint:     "A directory read answers with a file list, and an upload form while uploads are allowed",

	Start:          "Start server",
	Stop:           "Stop server",
	StartedFmt:     "Server started: %s",
	Stopped:        "Server stopped",
	StartFailedFmt: "The server could not start: %s",
	StopFailedFmt:  "The server could not stop: %s",
	InvalidPort:    "The port must be a number between 1 and 65535",
	InvalidRoot:    "The shared folder does not exist, or is not a folder",
}
