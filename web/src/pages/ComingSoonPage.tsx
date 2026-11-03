import { useTranslation } from 'react-i18next'
import { Hammer } from 'lucide-react'
import { Card, EmptyState, PageHeader } from '@/components/ui'

export type SoonSection = 'nodes' | 'routing' | 'logs' | 'amnezia' | 'settings'

export function ComingSoonPage({ section }: { section: SoonSection }) {
  const { t } = useTranslation()
  return (
    <>
      <PageHeader title={t(`nav.${section}`)} />
      <Card>
        <EmptyState icon={<Hammer className="h-6 w-6" />} title={t('soon.title')} text={t(`soon.${section}`)} />
      </Card>
    </>
  )
}
