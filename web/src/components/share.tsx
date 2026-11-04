import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import QRCode from 'qrcode'
import { Check, Copy, QrCode } from 'lucide-react'
import { Button } from './ui'

export function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard API needs a secure context; fall back to a hidden textarea.
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      ta.remove()
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <Button type="button" variant="secondary" size="sm" onClick={copy}>
      {copied ? <Check className="h-4 w-4 text-success" /> : <Copy className="h-4 w-4" />}
      {copied ? t('connect.copied') : t('connect.copy')}
    </Button>
  )
}

export function QrImage({ text, size = 220 }: { text: string; size?: number }) {
  const { t } = useTranslation()
  const [src, setSrc] = useState('')
  const [tooLong, setTooLong] = useState(false)
  useEffect(() => {
    let alive = true
    setTooLong(false)
    // Long keys (Amnezia vpn://) need the lowest error correction to fit, or do not fit at all.
    QRCode.toDataURL(text, { margin: 1, width: size * 2, errorCorrectionLevel: text.length > 1500 ? 'L' : 'M' })
      .then((url) => alive && setSrc(url))
      .catch(() => alive && setTooLong(true))
    return () => {
      alive = false
    }
  }, [text, size])
  if (tooLong) return <p className="text-sm text-muted-foreground">{t('connect.qrTooLong')}</p>
  return (
    <div className="inline-block rounded-xl bg-white p-2">
      {src ? <img src={src} width={size} height={size} alt="QR" /> : <div style={{ width: size, height: size }} />}
    </div>
  )
}

/** A link with copy and an optional QR code. */
export function ShareRow({ label, text, hint }: { label: string; text: string; hint?: string }) {
  const { t } = useTranslation()
  const [qr, setQr] = useState(false)
  return (
    <div className="rounded-xl border p-3.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm font-medium">{label}</div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={() => setQr(!qr)}>
            <QrCode className="h-4 w-4" />
            {qr ? t('connect.hideQr') : t('connect.showQr')}
          </Button>
          <CopyButton text={text} />
        </div>
      </div>
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
      <code className="mt-2 block truncate rounded-lg bg-muted px-2.5 py-1.5 text-xs text-muted-foreground" title={text}>
        {text}
      </code>
      {qr && (
        <div className="mt-3 flex justify-center">
          <QrImage text={text} />
        </div>
      )}
    </div>
  )
}
