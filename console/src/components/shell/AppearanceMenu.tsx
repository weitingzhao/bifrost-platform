import { Monitor, Moon, Palette, Sun } from 'lucide-react'
import { DropdownMenu as Menu } from 'radix-ui'
import { DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useConsoleAppearance, type Appearance, type VisualStyle } from '@/lib/shell/useConsoleAppearance'

export function AppearanceMenu() {
  const { appearance, style, dark, chooseAppearance, chooseStyle } = useConsoleAppearance()
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <button type="button" className="ops-appearance-trigger" aria-label="Appearance and style" title="Appearance and style">
        {dark ? <Moon size={19} aria-hidden /> : <Sun size={19} aria-hidden />}
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="ops-appearance-menu">
      <DropdownMenuLabel>Appearance</DropdownMenuLabel>
      <Menu.RadioGroup aria-label="Appearance" value={appearance} onValueChange={value => chooseAppearance(value as Appearance)}>
        {([{ value: 'light', label: 'Light', icon: Sun }, { value: 'dark', label: 'Dark', icon: Moon }, { value: 'system', label: 'Follow system', icon: Monitor }] as const).map(({ value, label, icon: Icon }) =>
          <Menu.RadioItem key={value} value={value} onSelect={event => event.preventDefault()}><Icon size={17} aria-hidden />{label}</Menu.RadioItem>)}
      </Menu.RadioGroup>
      <DropdownMenuSeparator />
      <DropdownMenuLabel>Visual style</DropdownMenuLabel>
      <Menu.RadioGroup aria-label="Visual style" value={style} onValueChange={value => chooseStyle(value as VisualStyle)}>
        {([{ value: 'gradient', label: 'Blue-violet' }, { value: 'grayscale', label: 'Grayscale' }] as const).map(({ value, label }) =>
          <Menu.RadioItem key={value} value={value} onSelect={event => event.preventDefault()}><Palette size={17} aria-hidden />{label}</Menu.RadioItem>)}
      </Menu.RadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>
}
