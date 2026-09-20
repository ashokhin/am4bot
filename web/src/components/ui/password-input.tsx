import { EyeIcon, EyeOffIcon } from 'lucide-react'
import { useState } from 'react'
import { cn } from '@/lib/utils'
import { Input } from './input'

// Every password field in the app should be this, not a raw
// `<Input type="password">` -- the toggle lets someone check what they
// actually typed before submitting, same as any modern password field.
export function PasswordInput({ className, ...props }: React.ComponentProps<typeof Input>) {
  const [visible, setVisible] = useState(false)

  return (
    <div className="relative">
      <Input type={visible ? 'text' : 'password'} className={cn('pr-9', className)} {...props} />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted-foreground hover:text-foreground"
        aria-label={visible ? 'Hide password' : 'Show password'}
        tabIndex={-1}
      >
        {visible ? <EyeOffIcon className="size-4" /> : <EyeIcon className="size-4" />}
      </button>
    </div>
  )
}
