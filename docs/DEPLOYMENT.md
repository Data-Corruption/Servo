# Deploy Servo docs

The site uses Hugo/Hextra and a separate Cloudflare Workers Static Assets project named `servo-docs`. Its canonical URL is `https://servo.sproutcli.dev/`. It must not deploy over Sprout's own documentation Worker.

## One-time setup

1. Set GitHub repository secrets `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` for the intended Cloudflare account; restrict the token to the needed Worker deployment permissions.
2. Keep `DOCS_ENABLED` unset until ready to publish. PR builds do not require these secrets or the deployment variable.
3. Set repository variable `DOCS_ENABLED=true`, then run the Docs workflow manually or push docs changes to main. Wrangler creates/deploys `servo-docs` from `docs/wrangler.jsonc`.
4. In that Worker's Domains & Routes settings, attach `servo.sproutcli.dev` as a custom domain in the account owning the `sproutcli.dev` zone. Confirm DNS, certificate issuance and the canonical URL before announcing the site.

Hugo, Hextra and Wrangler versions come from the existing pinned inputs. The release workflow and docs deployment gate are independent. Workflow dispatch and main pushes may publish; pull requests only build.

## Validation and recovery

Run the warning-fatal Hugo production build described in README.md before deployment. Run the pinned Wrangler `deploy --dry-run` from `docs/` to validate assets without publishing. Inspect deployment logs and the Worker identity before retrying a failed publish.

Verify the landing page, internal navigation, search, social preview, 404 page and HTTPS custom domain after publication. No Cloudflare or DNS changes are performed by a local build.

## Existing links

The public site now has three pages. `static/_redirects` maps the previous user-guide URLs to their new sections and retires developer-page URLs to the homepage. Workers Static Assets supports fragments in redirect destinations; see the [redirect format](https://developers.cloudflare.com/workers/static-assets/redirects/). The development references live in repository Markdown and are not published or indexed by site search. Check the redirects after deployment.
