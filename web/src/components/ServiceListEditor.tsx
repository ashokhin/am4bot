import { DndContext, closestCenter, type DragEndEvent, PointerSensor, KeyboardSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { CopyIcon, GripVerticalIcon, XIcon } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { ALL_SERVICES } from '../api/types'
import { cn } from '../lib/utils'
import { Button } from './ui/button'
import { Label } from './ui/label'
import { Switch } from './ui/switch'

interface Props {
  /** Enabled services, in the exact order they'll run in -- see Node.services' doc comment. A service may appear more than once (e.g. buying fuel both before and after departing) -- ambot runs whatever's in this array top to bottom, duplicates and all. */
  value: string[]
  onChange: (next: string[]) => void
}

/**
 * Every known service is always listed (per-service on/off switch), plus
 * the current run order below as a reorderable, duplicable list -- see
 * this component's Props doc comment on why duplicates are meaningful,
 * not a bug to prevent.
 */
export function ServiceListEditor({ value, onChange }: Props) {
  const { t } = useTranslation()
  const sensors = useSensors(useSensor(PointerSensor), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event
    if (!over || active.id === over.id) {
      return
    }

    const oldIndex = instanceIndexFromId(String(active.id))
    const newIndex = instanceIndexFromId(String(over.id))
    if (oldIndex === -1 || newIndex === -1) {
      return
    }

    const next = value.slice()
    const [moved] = next.splice(oldIndex, 1)
    next.splice(newIndex, 0, moved)
    onChange(next)
  }

  function toggle(service: string, enabled: boolean) {
    if (enabled) {
      onChange([...value, service])
    } else {
      onChange(value.filter((s) => s !== service))
    }
  }

  function duplicateAt(index: number) {
    const next = value.slice()
    next.splice(index + 1, 0, value[index])
    onChange(next)
  }

  function removeAt(index: number) {
    onChange(value.filter((_, i) => i !== index))
  }

  // Stable per-instance ids for dnd-kit: "service#instanceIndex" -- two
  // duplicate entries of the same service need distinct sortable ids.
  const instanceIds = value.map((service, i) => `${service}#${i}`)

  function instanceIndexFromId(id: string): number {
    return instanceIds.indexOf(id)
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Label>{t('nodes.services.catalogLabel')}</Label>
        <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
          {ALL_SERVICES.map((service) => (
            <ServiceToggleRow key={service} service={service} enabled={value.includes(service)} onToggle={toggle} />
          ))}
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('nodes.services.orderLabel')}</Label>
        {value.length === 0 && <p className="text-sm text-muted-foreground">{t('nodes.services.none')}</p>}
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={instanceIds} strategy={verticalListSortingStrategy}>
            <ul className="flex flex-col gap-1.5">
              {value.map((service, i) => (
                <SortableServiceItem
                  key={instanceIds[i]}
                  id={instanceIds[i]}
                  service={service}
                  onDuplicate={() => duplicateAt(i)}
                  onRemove={() => removeAt(i)}
                />
              ))}
            </ul>
          </SortableContext>
        </DndContext>
      </div>
    </div>
  )
}

function ServiceToggleRow({
  service,
  enabled,
  onToggle,
}: {
  service: string
  enabled: boolean
  onToggle: (service: string, enabled: boolean) => void
}) {
  const switchId = useId()

  return (
    <div className="flex items-center justify-between gap-2 rounded-md border px-3 py-2">
      <Label htmlFor={switchId} className="cursor-pointer font-normal">
        {service}
      </Label>
      <Switch id={switchId} checked={enabled} onCheckedChange={(v) => onToggle(service, v)} />
    </div>
  )
}

function SortableServiceItem({
  id,
  service,
  onDuplicate,
  onRemove,
}: {
  id: string
  service: string
  onDuplicate: () => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  }

  return (
    <li
      ref={setNodeRef}
      style={style}
      className={cn(
        'flex items-center justify-between gap-2 rounded-md border bg-card px-3 py-2',
        isDragging && 'opacity-50',
      )}
    >
      <span {...attributes} {...listeners} className="flex flex-1 cursor-grab items-center gap-2 text-sm" aria-label={t('nodes.services.dragHandle')}>
        <GripVerticalIcon className="size-4 text-muted-foreground" />
        {service}
      </span>
      <div className="flex items-center gap-1">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={onDuplicate}
          aria-label={t('nodes.services.duplicate', { service })}
          title={t('nodes.services.duplicate', { service })}
        >
          <CopyIcon className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={onRemove}
          aria-label={t('nodes.services.remove', { service })}
        >
          <XIcon className="size-3.5" />
        </Button>
      </div>
    </li>
  )
}
