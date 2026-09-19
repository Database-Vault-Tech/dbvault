import type { Metadata } from "next"

import { GeneralSettings } from "@/components/settings/general-settings"

export const metadata: Metadata = { title: "Settings" }

export default function SettingsPage() {
  return <GeneralSettings />
}
