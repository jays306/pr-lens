# github/client.go
DOES: GitHub REST client — fetches PR diff, metadata, file list, file contents, repo tree, review comments; posts reviews
TYPE: Client { token, httpClient }, TreeEntry { Path, Type }, ReviewComment { Path, Line, Body }
SYMBOLS: ParsePRURL(rawURL) → (*PRRef, error), NewClient(token) → *Client, FetchDiff(ctx, ref) → (string, error), FetchPRInfo(ctx, ref) → (*PRInfo, error), FetchPRFiles(ctx, ref) → ([]PRFile, error), FetchFileContents(ctx, ref, baseSHA, filenames) → []FileContent, FetchRepoTree(ctx, ref, sha) → ([]TreeEntry, error), FetchReviewComments(ctx, ref) → ([]ExistingComment, error), PostReview(ctx, ref, event, body, comments) → error
CALLED BY: handler.Analyze, handler.Review
