import { HelpCircleIcon } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover'

// A small "?" icon that shows an explanation ON CLICK, not hover -- easier
// to hit on touch devices, and doesn't fire accidentally while scanning a
// long settings form with the mouse. Used next to every advanced node
// setting to say what it does and which service(s) actually read it.
export function InfoHint({ text }: { text: string }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="inline-flex size-4 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
          aria-label="More information"
        >
          <HelpCircleIcon className="size-4" />
        </button>
      </PopoverTrigger>
      <PopoverContent>{text}</PopoverContent>
    </Popover>
  )
}
