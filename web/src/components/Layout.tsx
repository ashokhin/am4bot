import { LogOutIcon, MenuIcon, RadioIcon, ServerIcon, SettingsIcon, ShieldIcon, WifiIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { cn } from '../lib/utils'
import { LanguageSwitcher } from './LanguageSwitcher'
import { ThemeToggle } from './ThemeToggle'
import { Button } from './ui/button'
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from './ui/sheet'

interface NavItem {
  to: string
  labelKey: string
  icon: React.ComponentType<{ className?: string }>
}

const workspaceNav: NavItem[] = [
  { to: '/nodes', labelKey: 'nav.nodes', icon: ServerIcon },
  { to: '/metrics', labelKey: 'nav.metrics', icon: RadioIcon },
  { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
]

// An admin has no nodes of their own -- see requireNonAdminUser's doc
// comment -- so they get a different nav for that, but they still get
// their own Metrics (scoped to any user they pick) and Settings (display
// name + password, no VPN region section).
const adminNav: NavItem[] = [
  { to: '/users', labelKey: 'nav.users', icon: ShieldIcon },
  { to: '/admin/nodes', labelKey: 'nav.adminNodes', icon: ServerIcon },
  { to: '/admin/vpn', labelKey: 'nav.adminVpn', icon: WifiIcon },
  { to: '/admin/prometheus', labelKey: 'nav.adminPrometheus', icon: RadioIcon },
  { to: '/admin/metrics', labelKey: 'nav.metrics', icon: RadioIcon },
  { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
]

function NavLink({ item, onNavigate }: { item: NavItem; onNavigate?: () => void }) {
  const { t } = useTranslation()
  const location = useLocation()
  const active = location.pathname === item.to || location.pathname.startsWith(item.to + '/')
  const Icon = item.icon

  return (
    <Link
      to={item.to}
      onClick={onNavigate}
      className={cn(
        'flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors',
        active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
      )}
    >
      <Icon className="size-4 shrink-0" />
      {t(item.labelKey)}
    </Link>
  )
}

// Shared between the always-visible desktop sidebar and the mobile Sheet
// drawer -- onNavigate closes the drawer after a tap (irrelevant on
// desktop, where nothing renders it).
function NavList({ items, onNavigate }: { items: NavItem[]; onNavigate?: () => void }) {
  return (
    <nav className="flex flex-col gap-1">
      {items.map((item) => (
        <NavLink key={item.to} item={item} onNavigate={onNavigate} />
      ))}
    </nav>
  )
}

export function Layout({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const { t } = useTranslation()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  if (!user) {
    return <main>{children}</main>
  }

  // Forced onto /change-password by ProtectedRoute (see
  // User.must_change_password's doc comment) -- every nav link would just
  // bounce back there anyway, so don't show them at all. Logout stays
  // available in the header below in case they'd rather bail than change
  // it right now.
  const showNav = !user.must_change_password
  const items = user.is_admin ? adminNav : workspaceNav

  return (
    <div className="flex min-h-svh">
      {showNav && (
        // Always-visible sidebar from md up; below that it's replaced by
        // the Sheet drawer triggered from the header's hamburger button --
        // a fixed w-56 sidebar with no way to hide it would eat over half
        // of a phone-width screen permanently.
        <aside className="hidden w-56 shrink-0 flex-col gap-6 border-r bg-card px-3 py-4 md:flex">
          <div className="px-3 text-lg font-bold">am4bot</div>
          <NavList items={items} />
        </aside>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between gap-2 border-b bg-card px-4 py-3">
          <div className="flex min-w-0 items-center gap-2">
            {showNav && (
              <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
                <SheetTrigger asChild>
                  <Button variant="ghost" size="icon" className="md:hidden" aria-label={t('nav.openMenu')}>
                    <MenuIcon />
                  </Button>
                </SheetTrigger>
                <SheetContent side="left" className="md:hidden">
                  <SheetTitle>am4bot</SheetTitle>
                  <NavList items={items} onNavigate={() => setMobileNavOpen(false)} />
                </SheetContent>
              </Sheet>
            )}
            <div className="truncate text-sm text-muted-foreground">{t('nav.signedInAs', { login: user.login })}</div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <LanguageSwitcher />
            <ThemeToggle />
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('nav.logout')}
              onClick={() => {
                void logout()
              }}
            >
              <LogOutIcon />
            </Button>
          </div>
        </header>
        <main className="flex-1 overflow-x-hidden p-4 sm:p-6">{children}</main>
      </div>
    </div>
  )
}
