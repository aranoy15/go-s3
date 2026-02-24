# go-s3

Клиент для S3-совместимых хранилищ (AWS S3, Yandex Object Storage и т.п.) на базе AWS SDK v2.

## Установка

```bash
go get github.com/kjiumus/go-s3
```

## Конфигурация

```go
cfg := &s3.Config{
    Endpoint:        "https://storage.yandexcloud.net",
    AccessKeyID:     "...",
    SecretAccessKey: "...",
    BucketName:      "my-bucket",
    Region:          "ru-central1",
}
```

## Использование

```go
client, err := s3.New(cfg)
if err != nil {
    log.Fatal(err)
}

// Загрузка файла (возвращает presigned URL)
url, err := client.UploadFile(ctx, "objectID", "filename.jpg", body, "image/jpeg")

// Presigned URL по ключу
url, err := client.GetPresignedURL(ctx, "path/to/key", 15*time.Minute)

// Список presigned URL по префиксу
urls, err := client.GetObjects(ctx, "prefix/")

// Удаление
err := client.DeleteFile(ctx, "path/to/key")

// Проверка существования
exists, err := client.FileExists(ctx, "path/to/key")

// Поиск ключа по presigned URL
key, err := client.FindKeyByPresignedURL(ctx, presignedURL, "prefix/")
```

## Методы

- `New(cfg *Config) (*Client, error)` — создание клиента
- `UploadFile(ctx, objectID, key, body, contentType)` — загрузка, возвращает presigned URL
- `GetPresignedURL(ctx, key, expiration)` — presigned URL для скачивания
- `GetObjects(ctx, prefix)` — presigned URL всех объектов с префиксом
- `DeleteFile(ctx, key)` — удаление объекта
- `FileExists(ctx, key)` — проверка существования
- `FindKeyByPresignedURL(ctx, url, prefix)` — ключ по presigned URL
- `Bucket()`, `Endpoint()` — имя бакета и endpoint
