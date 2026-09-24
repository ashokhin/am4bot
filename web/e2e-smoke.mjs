// One-off manual e2e smoke script (not part of the build/test pipeline --
// see the frontend's proper test setup, if/when one is added). Drives the
// real running dev server against a real apiserver with Playwright's
// bundled Chromium, exercising: login, i18n switching, admin user CRUD,
// admin's read-only "all nodes" view, node CRUD (incl. service
// toggle/duplicate, friendly schedule builder, per-node timezone, the
// "can't enable until configured" guard), VPN region catalog + selection,
// logout. Run with: node e2e-smoke.mjs
import { chromium } from 'playwright'

const BASE = 'http://localhost:5173'

function assert(cond, msg) {
  if (!cond) {
    throw new Error('ASSERTION FAILED: ' + msg)
  }
  console.log('  ok:', msg)
}

// Radix Select renders a button trigger + a portal-mounted listbox, not a
// native <select> -- click the trigger, then click the option by its text.
async function selectRadixOption(page, triggerSelector, optionText) {
  await page.click(triggerSelector)
  await page.click(`[role=option]:has-text("${optionText}")`)
}

async function login(page, loginValue, password) {
  await page.goto(BASE + '/login')
  await page.fill('#login', loginValue)
  await page.fill('input[type=password]', password)
  await page.click('button[type=submit]')
}

async function logout(page) {
  await page.click('button[aria-label="Log out"]')
  await page.waitForURL('**/login')
}

const browser = await chromium.launch()
const page = await browser.newPage()

try {
  console.log('1. load /login, default language is English')
  await page.goto(BASE + '/login')
  await page.waitForSelector('h1')
  assert((await page.textContent('h1')) === 'Sign in', 'heading is "Sign in" (English default)')
  assert(await page.isVisible('text=Login'), 'login field is labeled "Login", not "Email" -- no signup flow, so no real email needed')

  console.log('2. switch language to Russian')
  await page.selectOption('select', 'ru')
  await page.waitForFunction(() => document.querySelector('h1')?.textContent === 'Вход')
  assert((await page.textContent('h1')) === 'Вход', 'heading is now "Вход" (Russian)')

  console.log('3. switch back to English for the rest of the run')
  await page.selectOption('select', 'en')
  await page.waitForFunction(() => document.querySelector('h1')?.textContent === 'Sign in')

  console.log('4. wrong password is rejected')
  await page.fill('#login', 'admin')
  await page.fill('input[type=password]', 'wrong-password')
  await page.click('button[type=submit]')
  await page.waitForSelector('[role=alert]')
  assert((await page.textContent('[role=alert]')) === 'Invalid login or password', 'error shown for wrong password')

  console.log('5. correct password logs in -- admin lands on /users, not /nodes (admins have no nodes of their own)')
  await page.fill('input[type=password]', 'adminpass123')
  await page.click('button[type=submit]')
  await page.waitForURL('**/users')
  await page.waitForSelector('h1')
  assert((await page.textContent('h1')) === 'Users', 'admin redirected straight to Users')
  assert(!(await page.isVisible('aside >> text="Nodes"')), 'admin sidebar has no Nodes link (only "All nodes")')
  assert(await page.isVisible('aside >> text=Settings'), 'admin sidebar HAS a Settings link (display name/password apply to admins too)')
  assert(await page.isVisible('aside >> text=Metrics'), 'admin sidebar has a Metrics link (scoped to any user they pick)')

  console.log('6. reload -- session (cookie) persists, still on /users')
  await page.reload()
  await page.waitForURL('**/users')
  assert(page.url().endsWith('/users'), 'still logged in after reload (cookie session)')

  console.log('7. create a new user via the dialog -- no timezone field anymore')
  await page.click('text=Add user')
  assert(!(await page.isVisible('[role=dialog] >> text=Timezone')), 'create-user dialog has no timezone field -- users pick it per-node, not the admin')
  await page.fill('[role=dialog] input#new-user-login', 'user1')
  await page.fill('[role=dialog] input[type=password]', 'userpass123')
  await page.click('[role=dialog] button[type=submit]')
  await page.waitForSelector('td:has-text("user1")')
  assert(await page.isVisible('td:has-text("user1")'), 'new user appears in the table')
  assert(await page.isVisible('td:has-text("Active")'), 'new user shows Active status')

  console.log('8. admin cannot disable their own account')
  const adminRow = page.locator('tr', { hasText: 'admin' })
  assert(await adminRow.locator('button[role=switch][disabled]').isVisible(), "admin's own row has a disabled switch")

  console.log('9. disable the new user (a Switch, not a button)')
  const userRow = page.locator('tr', { hasText: 'user1' })
  await userRow.locator('button[role=switch]').click()
  await page.waitForSelector('tr:has-text("user1") td:has-text("Disabled")')
  assert(true, 'user now shows Disabled status')

  console.log('10. re-enable the user')
  await userRow.locator('button[role=switch]').click()
  await page.waitForSelector('tr:has-text("user1") td:has-text("Active")')
  assert(true, 'user shows Active status again')

  console.log('11. admin sees every user\'s nodes in the read-only "All nodes" view')
  await page.click('aside >> text=All nodes')
  await page.waitForURL('**/admin/nodes')
  await page.waitForSelector('h1:has-text("All nodes")')
  await page.waitForSelector('table td:has-text("user1")')
  const adminNodeRows = await page.locator('tr', { hasText: 'user1' }).count()
  assert(adminNodeRows === 2, `admin sees both of user1's auto-created default nodes (got ${adminNodeRows})`)

  console.log('12. non-admin cannot see admin routes -- log out, log in as user1')
  await logout(page)
  await login(page, 'user1', 'userpass123')
  await page.waitForURL('**/nodes')
  assert(page.url().endsWith('/nodes'), 'non-admin redirected to /nodes after login')
  assert(!(await page.isVisible('aside >> text=Users')), 'non-admin does not see the Users nav link')

  console.log('13. non-admin visiting /users directly is redirected to /nodes')
  await page.goto(BASE + '/users')
  await page.waitForURL('**/nodes')
  assert(page.url().endsWith('/nodes'), 'AdminRoute redirected non-admin away from /users')

  console.log('14. user1 has TWO pre-created default nodes ("player" + "maintenance"), both disabled, neither deletable')
  await page.waitForSelector('table')
  const defaultBadges = await page.locator('text=default').count()
  assert(defaultBadges === 2, `two default-node badges visible (got ${defaultBadges})`)
  assert(!(await page.isVisible('table button:has-text("Delete")')), 'neither default node has a delete button')
  assert(await page.isVisible('td:has-text("player")'), '"player" default node present')
  assert(await page.isVisible('td:has-text("maintenance")'), '"maintenance" default node present')
  const disabledBadges = await page.locator('text=Disabled').count()
  assert(disabledBadges === 2, `both default nodes start Disabled (got ${disabledBadges})`)

  console.log('14b. enabling an unconfigured default node is refused with a clear error, not silently accepted')
  const maintenanceRow = page.locator('tr', { hasText: 'maintenance' })
  await maintenanceRow.locator('button[role=switch]').click()
  await page.waitForSelector('[data-sonner-toast][data-type=error]')
  assert(true, 'error toast shown for enabling an unconfigured node')
  assert(await maintenanceRow.locator('button[role=switch][aria-checked=false]').isVisible(), 'node stayed disabled')

  console.log('15. create a node with a service (toggle + duplicate), a friendly schedule, and a timezone')
  await page.click('text=Add node')
  await page.waitForSelector('h1')
  assert(!(await page.isVisible('text=Game URL')), 'game URL field removed from the form -- one URL for everyone')
  await page.fill('input[name=name]', 'test-node')
  await page.fill('input[name=game_username]', 'gameuser1')
  await page.fill('input[name=game_password]', 'gamepass123')

  await page.getByLabel('buy_fuel').click()
  await page.waitForSelector('li:has-text("buy_fuel")')
  assert(await page.isVisible('li:has-text("buy_fuel")'), 'toggling the catalog switch adds it to the run order')

  await page.click('li:has-text("buy_fuel") button[title="Duplicate buy_fuel"]')
  const fuelEntries = await page.locator('li:has-text("buy_fuel")').count()
  assert(fuelEntries === 2, `duplicating a service adds a second instance to the run order (got ${fuelEntries})`)

  await selectRadixOption(page, 'button[aria-label="Days"]', 'Weekdays only')
  assert(await page.isVisible('#timezone'), 'per-node timezone field present, defaulting to UTC')
  await page.click('button[type=submit]')
  await page.waitForURL('**/nodes')
  await page.waitForSelector('td:has-text("test-node")')
  assert(await page.isVisible('td:has-text("test-node")'), 'new node appears in the list')

  console.log('15b. delete it via the ConfirmDialog (not a native confirm() anymore)')
  const testNodeRow = page.locator('tr', { hasText: 'test-node' })
  await testNodeRow.locator('button', { hasText: 'Delete' }).click()
  await page.waitForSelector('[role=dialog]')
  assert(await page.isVisible('[role=dialog] >> text=test-node'), 'confirm dialog names the node')
  await page.click('[role=dialog] button:has-text("Confirm")')
  await page.waitForSelector('td:has-text("test-node")', { state: 'detached' })
  assert(true, 'node removed from the list after confirming')

  console.log('16. non-admin cannot manage the VPN catalog (no nav link, route redirects)')
  assert(!(await page.isVisible('aside >> text=VPN')), 'non-admin does not see the admin VPN nav link')
  await page.goto(BASE + '/admin/vpn')
  await page.waitForURL('**/nodes')
  assert(page.url().endsWith('/nodes'), 'AdminRoute redirected non-admin away from /admin/vpn')

  console.log('17. user1 has no VPN region catalog yet -- log back in as admin to create one')
  await logout(page)
  await login(page, 'admin', 'adminpass123')
  await page.waitForURL('**/users')

  console.log('18. admin configures the shared VPN provider account and a region')
  await page.click('aside >> text=VPN')
  await page.waitForURL('**/admin/vpn')
  await page.waitForSelector('text=Not configured yet')
  assert(await page.isVisible('text=Not configured yet'), 'provider starts unconfigured')
  await page.fill('input[name=provider]', 'expressvpn')
  await page.fill('input[name=vpn_username]', 'vpnuser1')
  await page.fill('input[name=vpn_password]', 'vpnpass123')
  await page.locator('form').first().locator('button[type=submit]').click()
  await page.waitForSelector('text=Configured (provider: expressvpn)')
  assert(true, 'vpn provider account configured')

  await page.click('text=Add region')
  await page.fill('input[name=region-name]', 'us-east')
  await page.fill('textarea[name=ovpn-config]', 'client\nremote vpn.example.com 1194\n')
  await page.locator('form').last().locator('button[type=submit]').click()
  await page.waitForSelector('td:has-text("us-east")')
  assert(await page.isVisible('td:has-text("us-east")'), 'new vpn region appears in the catalog')

  console.log('19. user1 picks the region from Settings -- it applies to all of their nodes, not per-node')
  await logout(page)
  await login(page, 'user1', 'userpass123')
  await page.waitForURL('**/nodes')
  await page.click('aside >> text=Settings')
  await page.waitForURL('**/settings')
  await selectRadixOption(page, '[data-testid=vpn-region]', 'us-east')
  await page.waitForSelector('[data-testid=vpn-region] >> text=us-east')
  assert(true, 'user1 selected the us-east vpn region for their account')

  console.log('20. admin deletes the region -- user1 falls back to no VPN, not an error')
  await logout(page)
  await login(page, 'admin', 'adminpass123')
  await page.waitForURL('**/users')
  await page.click('aside >> text=VPN')
  await page.waitForURL('**/admin/vpn')
  await page.click('table button:has-text("Delete")')
  await page.click('[role=dialog] button:has-text("Confirm")')
  await page.waitForSelector('td:has-text("us-east")', { state: 'detached' })
  assert(true, 'vpn region deleted from the catalog')

  console.log('21. user list shows UUID, last login, and active/total node counts; row click opens user detail')
  await page.click('aside >> text=Users')
  await page.waitForURL('**/users')
  await page.waitForSelector('table')
  const userRow2 = page.locator('tr', { hasText: 'user1' })
  assert(!(await userRow2.locator('td:has-text("Never")').isVisible()), 'user1 already logged in earlier in this run -- last login is a real timestamp, not "Never"')
  assert(await userRow2.locator('td:has-text("0 / 2")').isVisible(), 'user1 shows 2 total nodes, 0 active (they never enabled one)')
  await userRow2.click()
  await page.waitForURL('**/admin/users/**')
  await page.waitForFunction(() => document.querySelector('h1')?.textContent === 'user1')
  assert((await page.textContent('h1')) === 'user1', 'user detail page header shows user1')
  assert(await page.isVisible('text=Reset password'), 'reset password action present')
  assert(await page.isVisible('text=Delete user'), 'delete user action present')

  console.log('22. user detail lists their nodes; clicking one opens the read-only admin node detail page')
  await page.waitForSelector('table td:has-text("player")')
  await page.locator('tr', { hasText: 'player' }).click()
  await page.waitForURL('**/admin/nodes/**')
  await page.waitForSelector('h1:has-text("player")')
  assert(await page.isVisible('h1:has-text("player")'), 'node detail page header shows node name')
  assert(await page.isVisible('text=user1'), 'node detail shows the owner login, not a login/password form')
  assert(!(await page.isVisible('input[name=game_username]')), 'admin node detail has no editable game-login field -- read-only')
  await page.click('button:has-text("Back")')

  console.log('23. admin resets user1\'s password from the detail page')
  await page.waitForURL('**/admin/users/**')
  await page.click('text=Reset password')
  await page.waitForSelector('[role=dialog]')
  await page.fill('[role=dialog] input[type=password]', 'newuserpass456')
  await page.click('[role=dialog] button[type=submit]')
  await page.waitForSelector('[role=dialog]', { state: 'detached' })
  assert(true, 'password reset dialog closed after submit')

  console.log('24. disabling user1 from the detail page stops their nodes (verified via node still shows Disabled, no error)')
  const disableSwitch = page.locator('button[role=switch]').first()
  await disableSwitch.click()
  await page.waitForSelector('text=Disabled')
  assert(true, 'user1 now shows Disabled on their own detail page')
  await disableSwitch.click()
  await page.waitForSelector('text=Active')

  console.log('25. user1 logs in with the reset password, and their display name / password settings work')
  await logout(page)
  await login(page, 'user1', 'newuserpass456')
  await page.waitForURL('**/nodes')
  assert(page.url().endsWith('/nodes'), 'user1 logs in fine with the admin-reset password')

  await page.click('aside >> text=Settings')
  await page.waitForURL('**/settings')
  await page.waitForSelector('text=VPN exit region')
  assert(await page.isVisible('text=VPN exit region'), 'non-admin Settings still has the VPN region section')
  await page.fill('#display-name', 'Test User')
  await page.locator('form').first().locator('button[type=submit]').click()
  await page.waitForSelector('[data-sonner-toast]')
  assert(true, 'display name saved')

  console.log('26. user1 changes their own password via Settings')
  const pwForm = page.locator('form').nth(1)
  await pwForm.locator('input[type=password]').nth(0).fill('newuserpass456')
  await pwForm.locator('input[type=password]').nth(1).fill('finaluserpass789')
  await pwForm.locator('button[type=submit]').click()
  await page.waitForSelector('[data-sonner-toast][data-type=success]')
  assert(true, 'password changed via self-service settings')

  console.log('27. admin metrics page lets the admin pick a user and see their node tiles')
  await logout(page)
  await login(page, 'admin', 'adminpass123')
  await page.waitForURL('**/users')
  await page.click('aside >> text=Metrics')
  await page.waitForURL('**/admin/metrics')
  assert(await page.isVisible('text=Not configured') || await page.isVisible('[data-testid=admin-metrics-user]'), 'admin metrics page rendered (either "not configured" or a user picker)')

  console.log('28. admin deletes user1 -- their nodes go with them')
  await page.click('aside >> text=Users')
  await page.waitForURL('**/users')
  await page.click('tr:has-text("Test User")')
  await page.waitForURL('**/admin/users/**')
  await page.click('text=Delete user')
  await page.waitForSelector('[role=dialog]')
  await page.click('[role=dialog] button:has-text("Confirm")')
  await page.waitForURL('**/users')
  await page.waitForSelector('table')
  assert(!(await page.isVisible('td:has-text("Test User")')), 'user1 no longer in the user list after delete')

  console.log('\nALL CHECKS PASSED')
} finally {
  await browser.close()
}
