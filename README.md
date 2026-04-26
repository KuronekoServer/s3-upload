# s3-upload

S3互換ストレージにファイルをそのまま投げるための小さな HTTP サーバーです。

multipart/form-data で受けた file を、必要なら指定したディレクトリ配下へ元のファイル名のまま流し込みます。

## ざっくりできること

- POST /api/v1/upload で 単一または複数ファイルのアップロード
- path を指定するとそのディレクトリ配下へ、未指定ならバケット直下へ保存
- ファイル名は変えない
- GET /api/v1/files で JSON の一覧取得
- DELETE /api/v1/files?path=... で ファイルまたはディレクトリを削除
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
- PUBLIC_BASE_URL: レスポンスの url に使う公開ドメイン。例: https://s3.krnk.org
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

保存先を指定するなら path クエリを付けます。 ./ を付けてもそのまま使えます。

```bash
curl -X POST "http://localhost:8080/api/v1/upload?path=./archive/2026" \
  -F "file=@./sample.mp4"
```

複数ファイルを 1 リクエストで送るならこうです。

```bash
curl -X POST "http://localhost:8080/api/v1/upload?path=images/banner" \
  -F "files=@./image-a.png" \
  -F "files=@./image-b.png"
```

一覧取得はこうです。

```bash
curl http://localhost:8080/api/v1/files
```

一覧は JSON で返り、ディレクトリは isDirectory=true になります。

削除は path クエリに対象パスをそのまま入れる形を基本にします。

```bash
curl -X DELETE "http://localhost:8080/api/v1/files?path=sample.mp4"
```

ディレクトリ削除も同じです。末尾に / を付けるとディレクトリとして明示できます。

```bash
curl -X DELETE "http://localhost:8080/api/v1/files?path=archive/2026/"
```

path クエリはファイルでもディレクトリでも同じ入口なので、ネストしたパスでもそのまま指定できます。末尾に / がない場合でも、配下にオブジェクトがあればディレクトリとして自動判定します。

認証ありならヘッダーを足します。

```bash
curl -X POST http://localhost:8080/api/v1/upload \
  -H "X-Auth-Key: your-secret-key" \
  -F "file=@./sample.mp4"
```

一覧や削除でも同じように X-Auth-Key を付ければ使えます。

アップロード成功時は success, message, files が返ります。

files の各要素には name, key, url, contentType が入ります。key と url には path 指定後の保存先が反映されます。単一アップロードでも files は配列です。

PUBLIC_BASE_URL を設定すると、返却される url は S3_ENDPOINT ではなくその公開ドメインを基準に組み立てられます。

一覧取得は success と files を返し、各 file には key, name, url, size, lastModified, isDirectory が入ります。

削除は success, message, target, kind, deletedCount を返します。

## ドキュメント

- ブラウザで見る: /docs
- 仕様をそのまま取る: /openapi.json

Try it out を使ってそのままアップロード確認もできます。

Bunny Shield API Guardianにopenapi.jsonぶん投げると使えるらしいのでやってみようかなと

[BunnyCDN アフェリエイトリンク](<https://bunny.net?ref=hhdqsy3idp>)

## Linux に置くなら

setup.sh は Linux 専用です。最新リリースを落として、systemd のサービスまで作る想定になっています。