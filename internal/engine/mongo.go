package engine

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

func dumpMongo(ctx context.Context, src Source, output io.Writer) (Format, error) {
	parallel := src.Parallel
	if parallel < 1 {
		parallel = 1
	}
	if parallel > 16 {
		parallel = 16
	}
	args := []string{uriArg(src), "--archive", "--gzip"}
	for _, name := range src.ExcludeTables {
		args = append(args, "--excludeCollection="+name)
	}
	if parallel > 1 {
		args = append(args, "--numParallelCollections="+strconv.Itoa(parallel))
	}
	if err := run(ctx, "mongodump", args, nil, nil, output); err != nil {
		return Format{}, fmt.Errorf("mongodump: %s", redact(redact(err.Error(), src.Password), uriArg(src)))
	}
	return Format{Name: "mongo-archive", Extension: ".archive.gz", CompressInPipeline: false}, nil
}

func restoreMongo(ctx context.Context, src Source, input io.Reader) error {
	args := []string{uriArg(src), "--archive", "--gzip", "--drop"}
	err := run(ctx, "mongorestore", args, nil, input, io.Discard)
	if err != nil {
		return fmt.Errorf("mongorestore: %s", redact(redact(err.Error(), src.Password), uriArg(src)))
	}
	return nil
}

func uriArg(src Source) string {
	user := url.QueryEscape(src.User)
	password := url.QueryEscape(src.Password)
	return "--uri=mongodb://" + user + ":" + password + "@" + src.Host + ":" + strconv.Itoa(src.Port) + "/" + url.PathEscape(src.Database)
}

func PingMongo(ctx context.Context, src Source) error {
	args := []string{uriArg(src), "--archive", "--gzip", "--collection=__dmsx_connection_probe__"}
	err := run(ctx, "mongodump", args, nil, nil, io.Discard)
	if err != nil {
		return fmt.Errorf("mongodump: %s", redact(redact(err.Error(), src.Password), uriArg(src)))
	}
	return nil
}
