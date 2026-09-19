"use client"

import { useQueryClient } from "@tanstack/react-query"
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react"

import { ORG_STORAGE_KEY } from "./api"
import { useMe } from "./queries"
import type { Me, Organization, Role } from "./types"

interface OrgContextValue {
  me: Me
  org: Organization
  orgs: Organization[]
  switchOrg: (id: string) => void
  /** Whether the current user's role in this organization is at least min. */
  can: (min: Role) => boolean
}

const OrgContext = createContext<OrgContextValue | null>(null)

const RANK: Record<Role, number> = { viewer: 1, member: 2, admin: 3, owner: 4 }

export function roleAtLeast(role: Role | undefined, min: Role): boolean {
  return !!role && RANK[role] >= RANK[min]
}

function readStored(): string | null {
  try {
    return window.localStorage.getItem(ORG_STORAGE_KEY)
  } catch {
    return null
  }
}

function writeStored(id: string) {
  try {
    window.localStorage.setItem(ORG_STORAGE_KEY, id)
  } catch {
    // Private mode: the API falls back to the first organization.
  }
}

/**
 * Resolves the current organization. Children only render once the user
 * and organization are known, so every page can rely on useOrg().
 */
export function OrgProvider({ me, children }: { me: Me; children: ReactNode }) {
  const qc = useQueryClient()
  const [selected, setSelected] = useState<string | null>(() => (typeof window === "undefined" ? null : readStored()))

  const org = useMemo(
    () => me.organizations.find((o) => o.id === selected) ?? me.organizations[0],
    [me.organizations, selected],
  )

  useEffect(() => {
    if (org && org.id !== readStored()) writeStored(org.id)
  }, [org])

  const switchOrg = useCallback(
    (id: string) => {
      writeStored(id)
      setSelected(id)
      // Reload org-scoped data with the new X-DBVault-Org header.
      void qc.resetQueries({
        predicate: (q) => !["me", "system", "sessions", "tokens"].includes(String(q.queryKey[0])),
      })
    },
    [qc],
  )

  const value = useMemo<OrgContextValue>(
    () => ({ me, org, orgs: me.organizations, switchOrg, can: (min) => roleAtLeast(org?.role, min) }),
    [me, org, switchOrg],
  )
  if (!org) return null
  return <OrgContext.Provider value={value}>{children}</OrgContext.Provider>
}

export function useOrg(): OrgContextValue {
  const ctx = useContext(OrgContext)
  if (!ctx) throw new Error("useOrg must be used inside OrgProvider")
  return ctx
}

/** Loads the session for the app shell. */
export function useSession() {
  return useMe()
}
