"use client"

import { Check, ChevronsUpDown, Plus } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/ui/spinner"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useCreateOrganization } from "@/lib/queries"

function initials(name: string) {
  return name
    .split(/\s+/)
    .map((p) => p[0])
    .join("")
    .slice(0, 2)
    .toUpperCase()
}

export function OrgAvatar({ name }: { name: string }) {
  return (
    <span className="flex size-5 shrink-0 items-center justify-center rounded-md bg-foreground text-[10px] font-semibold text-background">
      {initials(name)}
    </span>
  )
}

export function OrgSwitcher() {
  const { org, orgs, switchOrg } = useOrg()
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")
  const create = useCreateOrganization()

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" className="h-8 max-w-56 gap-2 px-2">
            <OrgAvatar name={org.name} />
            <span className="truncate font-medium">{org.name}</span>
            <span className="hidden text-xs text-muted-foreground capitalize sm:inline">{org.role}</span>
            <ChevronsUpDown className="text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel className="text-xs text-muted-foreground">Organizations</DropdownMenuLabel>
          {orgs.map((o) => (
            <DropdownMenuItem key={o.id} onSelect={() => o.id !== org.id && switchOrg(o.id)}>
              <OrgAvatar name={o.name} />
              <span className="truncate">{o.name}</span>
              {o.id === org.id && <Check className="ml-auto" />}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setCreating(true)}>
            <Plus /> New organization
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault()
              create.mutate(name.trim(), {
                onSuccess: (o) => {
                  toast.success(`Created ${o.name}`)
                  setCreating(false)
                  setName("")
                  switchOrg(o.id)
                },
                onError: (err) => toast.error(errorMessage(err)),
              })
            }}
          >
            <DialogHeader>
              <DialogTitle>New organization</DialogTitle>
              <DialogDescription>Organizations keep databases, storage, backups and teammates separate. Each has its own encryption key.</DialogDescription>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor="org-name">Name</Label>
              <Input id="org-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Inc." maxLength={80} autoFocus />
            </div>
            <DialogFooter>
              <Button type="submit" disabled={!name.trim() || create.isPending}>
                {create.isPending && <Spinner />} Create organization
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
