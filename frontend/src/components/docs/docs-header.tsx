"use client"

import Link from "next/link"
import { useSyncExternalStore } from "react"

import { ThemeToggle } from "@/components/app/theme-toggle"
import { Logo } from "@/components/brand/logo"
import { GITHUB_URL } from "@/components/marketing/constants"
import { GitHubIcon } from "@/components/marketing/github-icon"
import { Button } from "@/components/ui/button"

import { DocsMobileNav } from "./docs-nav"

// The session cookie is HttpOnly; its readable CSRF twin tells us someone is
// signed in, so we can offer the dashboard instead of "Sign in". The static
// HTML (server snapshot) always shows "Sign in".
const noSubscription = () => () => {}
const hasSession = () => document.cookie.split("; ").some((c) => c.startsWith("dbvault_csrf="))

/** Header for the public docs: works whether or not the reader is signed in. */
export function DocsHeader() {
  const signedIn = useSyncExternalStore(noSubscription, hasSession, () => false)

  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-background/80 backdrop-blur-md supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-16 w-full max-w-7xl items-center gap-3 px-4 sm:px-6 lg:px-8">
        <DocsMobileNav />
        <Link href="/" aria-label="DBVault home" className="shrink-0">
          <Logo />
        </Link>
        <span className="text-muted-foreground/60" aria-hidden>
          /
        </span>
        <Link href="/docs" className="text-sm font-medium">
          Docs
        </Link>
        <div className="ml-auto flex items-center gap-1.5">
          <ThemeToggle />
          <Button variant="ghost" size="icon" asChild className="hidden sm:inline-flex">
            <a href={GITHUB_URL} target="_blank" rel="noreferrer" aria-label="DBVault on GitHub">
              <GitHubIcon className="size-4" />
            </a>
          </Button>
          {signedIn ? (
            <Button asChild size="sm">
              <Link href="/dashboard">Dashboard</Link>
            </Button>
          ) : (
            <Button asChild size="sm" variant="outline">
              <Link href="/login">Sign in</Link>
            </Button>
          )}
        </div>
      </div>
    </header>
  )
}
