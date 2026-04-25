# s3-upload

S3互換ストレージにファイルをそのまま投げるための小さな HTTP サーバーです。

multipart/form-data で受けた file を uploads/{filename} に流し込みます。

## ざっくりできること

- POST /api/v1/upload で ファイルアップロード
- 必要なら X-Auth-Key ヘッダーで簡単な認証

## ローカルで動かす

まずは .env.example を .env にコピーして、中身を埋めます。

```bash
cp .env.example .env
```

そのあとビルドして起動します。

```bash
go build -o s3-upload
./s3-upload
```

Windows ならこんな感じです。

```powershell
Copy-Item .env.example .env
go build -o s3-upload.exe
.\s3-upload.exe
```

## 必須の設定

最低限これだけ入っていれば起動できます。

- S3_ACCESS_KEY
- S3_SECRET_KEY
- S3_BUCKET
- S3_ENDPOINT

.env.example で詳細は確認をお願いします。

- S3_REGION: 未指定なら us-east-1
- PORT: 未指定なら 8080
- MAX_CONCURRENT_UPLOADS: 同時アップロード数。未指定なら 10
- UPLOAD_PART_SIZE_MB: 1 パートのサイズ。未指定なら 32
- UPLOAD_CONCURRENCY: 1 ファイル内の並列数。未指定なら 8
- AUTH_ENABLED: true にすると X-Auth-Key 必須
- AUTH_KEY: AUTH_ENABLED=true の時に必要

## API の使い方

認証なしならこれだけです。

```bash
curl -X POST http://localhost:8080/api/v1/upload \
  -F "file=@./sample.mp4"
```

認証ありならヘッダーを足します。

```bash
curl -X POST http://localhost:8080/api/v1/upload \
  -H "X-Auth-Key: your-secret-key" \
  -F "file=@./sample.mp4"
```

成功すると JSON で success, message, url, key が返ります。

## ドキュメント

- ブラウザで見る: /docs
- 仕様をそのまま取る: /openapi.json

Try it out を使ってそのままアップロード確認もできます。

openapi形式でBunny Shield API Guardianが使えるらしいのでやってみようかなと

[BunnyCDN アフェリエイトリンク](<https://bunny.net?ref=hhdqsy3idp>)

## Linux に置くなら

setup.sh は Linux 専用です。最新リリースを落として、systemd のサービスまで作る想定になっています。