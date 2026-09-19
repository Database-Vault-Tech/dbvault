"use client"

import { Database, Plus, Search } from "lucide-react"
import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { Kbd } from "@/components/ui/kbd"
import { useDatabases } from "@/lib/queries"

import { primaryNav, secondaryNav } from "./nav"

export function CommandMenu() {
  const [open, setOpen] = useState(false)
  const router = useRouter()
  const { data: databases } = useDatabases()

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  const go = (href: string) => {
    setOpen(false)
    router.push(href)
  }

  return (
    <>
      <Button
        variant="outline"
        onClick={() => setOpen(true)}
        className="h-8 w-full max-w-72 justify-start gap-2 px-2.5 font-normal text-muted-foreground sm:w-64"
      >
        <Search />
        <span className="flex-1 text-left">Search…</span>
        <Kbd className="hidden sm:inline-flex">⌘K</Kbd>
      </Button>
      <CommandDialog open={open} onOpenChange={setOpen} title="Search DBVault" description="Jump to a page, database or action">
        <CommandInput placeholder="Search pages, databases and actions…" />
        <CommandList>
          <CommandEmpty>No results.</CommandEmpty>
          <CommandGroup heading="Actions">
            <CommandItem onSelect={() => go("/databases/new")}>
              <Plus /> Add database
            </CommandItem>
            <CommandItem onSelect={() => go("/storage?new=1")}>
              <Plus /> Add storage destination
            </CommandItem>
            <CommandItem onSelect={() => go("/schedules?new=1")}>
              <Plus /> Create schedule
            </CommandItem>
          </CommandGroup>
          {!!databases?.length && (
            <>
              <CommandSeparator />
              <CommandGroup heading="Databases">
                {databases.map((d) => (
                  <CommandItem key={d.id} value={`database ${d.name} ${d.host}`} onSelect={() => go(`/databases/${d.id}`)}>
                    <Database />
                    <span>{d.name}</span>
                    <span className="ml-auto truncate font-mono text-xs text-muted-foreground">
                      {d.host}/{d.database}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </>
          )}
          <CommandSeparator />
          <CommandGroup heading="Pages">
            {[...primaryNav, ...secondaryNav, { label: "Security", href: "/settings/security", icon: secondaryNav[2].icon }].map((item) => (
              <CommandItem key={item.href} onSelect={() => go(item.href)}>
                <item.icon /> {item.label}
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </>
  )
}
