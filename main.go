package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// 配置信息 - 已删除 Rsync 相关字段
type Config struct {
	Port     string `yaml:"port"`
	RootDir  string `yaml:"root_dir"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// 文件信息
type FileInfo struct {
	Name        string
	Path        string
	Size        int64
	IsDir       bool
	ModTime     time.Time
	Permissions string
}

// 模板数据结构
type TemplateData struct {
	Path       string
	ParentPath string
	FileList   []map[string]interface{}
}

var config Config

func init() {
	// 配置Viper
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AutomaticEnv()

	// 设置默认值 - 已删除 Rsync 相关配置
	viper.SetDefault("port", "8080")
	viper.SetDefault("root_dir", "./mirror")
	viper.SetDefault("username", "admin")
	viper.SetDefault("password", "password")

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 配置文件不存在，使用默认值
			fmt.Println("Config file not found, using default values")
		} else {
			// 配置文件存在但有错误
			panic(fmt.Sprintf("Failed to read config file: %v", err))
		}
	}

	// 直接从Viper获取值 - 已删除 Rsync 相关配置
	config.Port = viper.GetString("port")
	config.RootDir = viper.GetString("root_dir")
	config.Username = viper.GetString("username")
	config.Password = viper.GetString("password")

	// 调试信息
	fmt.Printf("Loaded config: Port=%s, RootDir=%s, Username=%s\n",
		config.Port, config.RootDir, config.Username)

	// 创建根目录
	if err := os.MkdirAll(config.RootDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create root directory: %v", err))
	}
}

// 基本认证中间件
func basicAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, password, ok := c.Request.BasicAuth()
		if !ok || username != config.Username || password != config.Password {
			c.Header("WWW-Authenticate", "Basic realm=\"Restricted\"")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// 获取文件列表
func getFileList(path string) ([]FileInfo, error) {
	fullPath := filepath.Join(config.RootDir, path)
	files, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var fileList []FileInfo
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			continue
		}
		fileInfo := FileInfo{
			Name:        file.Name(),
			Path:        filepath.Join(path, file.Name()),
			Size:        info.Size(),
			IsDir:       file.IsDir(),
			ModTime:     info.ModTime(),
			Permissions: info.Mode().String(),
		}
		fileList = append(fileList, fileInfo)
	}

	return fileList, nil
}

// 格式化文件大小
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// 格式化时间
func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// 首页处理函数
func indexHandler(c *gin.Context) {
	path := c.Query("path")
	fullPath := filepath.Join(config.RootDir, path)

	// 检查路径是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Path not found"})
		return
	}

	// 获取文件列表
	fileList, err := getFileList(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 准备模板数据
	parentPath := ""
	if path != "" {
		parentPath = filepath.Dir(path)
		if parentPath == "." {
			parentPath = ""
		}
	}

	// 格式化文件信息
	var formattedFileList []map[string]interface{}
	for _, file := range fileList {
		formattedFile := map[string]interface{}{
			"Name":          file.Name,
			"Path":          file.Path,
			"IsDir":         file.IsDir,
			"FormattedSize": formatSize(file.Size),
			"FormattedTime": formatTime(file.ModTime),
		}
		formattedFileList = append(formattedFileList, formattedFile)
	}

	// 渲染模板
	data := TemplateData{
		Path:       path,
		ParentPath: parentPath,
		FileList:   formattedFileList,
	}
	c.HTML(http.StatusOK, "mirror.html", data)
}

// 下载文件（查询参数方式）
func downloadHandler(c *gin.Context) {
	path := c.Query("path")
	fullPath := filepath.Join(config.RootDir, path)

	// 检查文件是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// 设置下载头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	c.Header("Content-Type", "application/octet-stream")
	c.File(fullPath)
}

// 下载文件（路径参数方式）
func downloadHandlerWithPath(c *gin.Context) {
	// 获取路径参数并移除开头的斜杠
	path := strings.TrimPrefix(c.Param("path"), "/")
	fullPath := filepath.Join(config.RootDir, path)

	// 检查文件是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// 设置下载头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	c.Header("Content-Type", "application/octet-stream")
	c.File(fullPath)
}

func main() {
	// 设置为生产模式
	gin.SetMode(gin.ReleaseMode)

	// 创建gin引擎
	r := gin.Default()

	// 初始化模板引擎 - 只加载HTML文件
	r.LoadHTMLGlob("templates/*.html")
	// 静态文件服务
	r.Static("/static", "./static")
	// 添加favicon支持
	r.StaticFile("/favicon.ico", "./favicon.ico")
	// 路由 - 已删除 rsync 相关路由
	r.GET("/", basicAuth(), indexHandler)
	r.GET("/download", basicAuth(), downloadHandler)
	r.GET("/download/*path", basicAuth(), downloadHandlerWithPath)

	// 启动服务器
	fmt.Printf("Server is running on http://localhost:%s\n", config.Port)
	if err := r.Run(":" + config.Port); err != nil {
		panic(fmt.Sprintf("Failed to start server: %v", err))
	}
}
