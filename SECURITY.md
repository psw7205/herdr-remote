# Security Policy

> **English summary.** Please report vulnerabilities privately through GitHub's
> **Security → Report a vulnerability** form for this repository. Do not open a public issue.
> This is a personal project maintained on a best-effort basis; there is no response SLA.
> Only the latest commit on `main` is supported.

## 신고 방법

취약점은 공개 issue가 아니라 이 repo의 GitHub **Security → Report a vulnerability**로 비공개
신고한다. 개인 project라 응답 시한은 보장하지 않는다. 지원 대상은 `main`의 최신 commit이다.
Herdr patch(fork `psw7205/herdr`의 `mobile-binding`)에서 비롯된 문제도 이 repo로 신고한다.

신고에는 다음을 포함한다.

- 영향과 재현 절차
- 이 repo의 commit, Herdr patch commit, Claude Code version
- 필요하면 `doctor` 출력. tailnet host, login, 절대 경로는 [README](README.md#진단-출력-공유)처럼
  placeholder로 바꾼다

transcript 원문, 실제 prompt, binding token은 신고에 붙이지 않는다.

## 보안 모델

Bridge는 single-user, single-host tool이다.

- 항상 loopback에만 bind한다. 원격 경로는 Tailscale Serve 하나다.
- 모든 요청의 `Host`를 loopback 또는 확정된 tailnet host로 제한한다.
- Serve 경유 요청은 `Tailscale-User-Login`이 node 소유자와 같아야 한다.
- HTTP mutation과 WebSocket은 exact Origin allowlist를 검사한다.
- 자체 password/OAuth/JWT는 없다.

자세한 경계는 [README 보안 경계](README.md#보안-경계)와
[ADR-024](docs/adr.md#adr-024--tailnet을-primary-security-boundary로-사용한다)를 따른다.

## 신고 대상 예시

- Host, Origin, Tailscale identity 검사를 우회해 소유자가 아닌 사용자가 읽거나 제어하는 경우
- 같은 `command_id`가 두 번 dispatch되거나, 오래된 `runtime_binding`으로 다른 process에 입력되는 경우
- transcript 본문, prompt, binding token이 응답, log, 진단 출력에 노출되는 경우
- static file serving의 path traversal이나 directory listing
- `doctor`가 Funnel이나 identity를 우회하는 Serve forward를 blocker로 보고하지 못하는 경우

## 범위 밖

- 지원하지 않는 구성: Tailscale Funnel, Serve 외의 reverse proxy, public internet 노출, multi-user
- Bridge host에 이미 local 접근 권한이 있는 사용자와 process. loopback 요청은 신뢰 경계 안이다
- Tailscale, Herdr upstream, Claude Code 자체의 취약점. 각 upstream에 신고한다
