import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Timeline, Toast,
  ActionBar, PhotoStrip, Field, ChoiceRow, PhoneIcon, ChatIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { staff, tickets } from '../src/data/mock';

/**
 * One job — triage, quote, dispatch, and the owner-approval boundary.
 *
 * The boundary is the point of this screen: anything inside the manager's
 * spend authority can be committed here and now; anything above it can only
 * be sent to the owner, and the app says so plainly rather than failing later.
 */

const statusTone: Record<string, Tone> = {
  New: 'blue', Triaged: 'violet', Quoted: 'amber', Scheduled: 'blue',
  'In progress': 'amber', 'Awaiting owner': 'violet', Resolved: 'green',
};

export default function TicketScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const isNew = id === 'new';
  const t = tickets.find((x) => x.id === id) ?? tickets[0];

  const [toast, setToast] = useState<string | null>(null);
  const [liability, setLiability] = useState(t.liability);
  const [note, setNote] = useState('');

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  if (isNew) return <NewJob onBack={() => router.back()} />;

  const overAuthority = (t.quotePaise ?? 0) > staff.spendAuthorityPaise;

  return (
    <>
      <BackHeader title={t.title} subtitle={`${t.id.toUpperCase()} · ${t.unit}`} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={s.row}>
            <StatusPill text={t.status} tone={statusTone[t.status]} />
            <StatusPill text={t.priority} tone={t.priority === 'Emergency' ? 'red' : t.priority === 'Urgent' ? 'amber' : 'neutral'} />
            <View style={{ flex: 1 }} />
            <Text style={[s.sla, t.slaLeft.includes('Breach') && { color: color.negative }]}>{t.slaLeft}</Text>
          </View>

          <Text style={s.detail}>{t.detail}</Text>
          {t.photos ? <PhotoStrip count={t.photos} onAdd={() => say('Camera would open here')} /> : null}

          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Reported by" v={t.tenant} />
            <KeyValue k="Raised" v={t.raised} />
            <KeyValue k="Category" v={t.category} />
            <KeyValue k="SLA" v={`${t.slaHours} hours from report`} last />
          </View>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Button label="Call tenant" tone="secondary" small icon={<PhoneIcon size={17} c={color.accent} />} onPress={() => say('Call logged')} style={{ flex: 1 }} />
            <Button label="Message" tone="secondary" small icon={<ChatIcon size={17} c={color.accent} />} onPress={() => router.push('/thread?id=th1')} style={{ flex: 1 }} />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Who bears the cost</Text>
          {(['Owner', 'Tenant', 'Shared'] as const).map((l, i) => (
            <ChoiceRow
              key={l}
              label={l}
              hint={
                l === 'Owner' ? 'Asset defect or fixture failure — recharged to the owner statement'
                : l === 'Tenant' ? 'Wear, misuse or below the agreement threshold — added to the next invoice'
                : 'Split by the agreement; both parties see the split before work starts'
              }
              selected={liability === l}
              onPress={() => { setLiability(l); say(`Liability set to ${l.toLowerCase()}`); }}
              last={i === 2}
            />
          ))}
        </Card>

        {t.quotePaise ? (
          <Card>
            <Text style={s.h}>Quote</Text>
            <KeyValue k="Vendor" v={t.vendor ?? '—'} />
            <KeyValue k="Amount" v={inr(t.quotePaise)} />
            <KeyValue k="Your authority" v={inr(staff.spendAuthorityPaise, { noPaise: true })} tone={overAuthority ? 'red' : 'green'} last />
            {overAuthority ? (
              <View style={s.warn}>
                <Text style={s.warnText}>
                  Above your authority. You can send it to the owner for approval, but you cannot
                  instruct the vendor to start.
                </Text>
              </View>
            ) : null}
            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
              {overAuthority ? (
                <Button label="Send to owner" onPress={() => say('Sent to the owner for approval')} style={{ flex: 1 }} />
              ) : (
                <Button label="Approve and instruct" onPress={() => say('Approved — vendor instructed')} style={{ flex: 1 }} />
              )}
              <Button label="Re-quote" tone="secondary" onPress={() => router.push(`/dispatch?ticket=${t.id}`)} style={{ flex: 1 }} />
            </View>
          </Card>
        ) : null}

        <Card>
          <Text style={s.h}>Vendor</Text>
          {t.vendor ? (
            <>
              <KeyValue k="Assigned" v={t.vendor} />
              <KeyValue k="Visit" v={t.status === 'Scheduled' ? '30 Jul, 10:00 – 12:00' : 'Not booked'} last />
              <Button label="Change vendor" tone="secondary" onPress={() => router.push(`/dispatch?ticket=${t.id}`)} style={{ marginTop: space(4) }} />
            </>
          ) : (
            <>
              <Text style={s.sub}>No vendor assigned. The SLA clock does not stop for that.</Text>
              <Button label="Dispatch a vendor" onPress={() => router.push(`/dispatch?ticket=${t.id}`)} style={{ marginTop: space(4) }} />
            </>
          )}
        </Card>

        <Card>
          <Text style={s.h}>Add an update</Text>
          <Field label="Visible to the tenant" value={note} onChange={setNote} placeholder="Plumber is on the way…" multiline />
          <Button label="Post update" tone="secondary" onPress={() => { setNote(''); say('Update posted to the tenant'); }} />
        </Card>

        <Card>
          <Text style={s.h}>Timeline</Text>
          <Timeline items={t.timeline} />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Resolve" tone="secondary" onPress={() => say('Job marked resolved')} style={{ flex: 1 }} />
        <Button label="Dispatch" onPress={() => router.push(`/dispatch?ticket=${t.id}`)} style={{ flex: 1 }} />
      </ActionBar>
    </>
  );
}

function NewJob({ onBack }: { onBack: () => void }) {
  const [title, setTitle] = useState('');
  const [unit, setUnit] = useState('');
  const [detail, setDetail] = useState('');
  const [priority, setPriority] = useState('Routine');
  const [saved, setSaved] = useState(false);

  return (
    <>
      <BackHeader title="Log a job" onBack={onBack} />
      <Screen>
        {saved ? <Toast text="Job created and queued to sync" /> : null}
        <Card>
          <Field label="What is wrong" value={title} onChange={setTitle} placeholder="Bathroom tap leaking" />
          <Field label="Unit" value={unit} onChange={setUnit} placeholder="Flat 402, Brigade Palm Grove" />
          <Field label="Detail" value={detail} onChange={setDetail} placeholder="What you saw, and what you have already tried" multiline />
        </Card>
        <Card>
          <Text style={s.h}>Priority</Text>
          {['Emergency', 'Urgent', 'Routine'].map((p, i) => (
            <ChoiceRow
              key={p}
              label={p}
              hint={p === 'Emergency' ? '4 hour SLA' : p === 'Urgent' ? '24 hour SLA' : '72 hour SLA'}
              selected={priority === p}
              onPress={() => setPriority(p)}
              last={i === 2}
            />
          ))}
        </Card>
        <Card>
          <Text style={s.h}>Evidence</Text>
          <PhotoStrip count={0} onAdd={() => {}} />
          <Text style={s.sub}>Photos capture offline and upload when the signal returns.</Text>
        </Card>
        <Button
          label="Create job"
          onPress={() => setSaved(true)}
          disabled={!title || !unit}
          style={{ marginHorizontal: space(4), marginTop: space(2) }}
        />
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  sla: { ...font.small, color: color.inkSoft, fontWeight: '700' },
  detail: { ...font.body, color: color.ink, lineHeight: 22, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 6, lineHeight: 18 },
  warn: { backgroundColor: '#FDEBE4', borderRadius: 10, padding: space(3), marginTop: space(3) },
  warnText: { ...font.small, color: '#C4501F', lineHeight: 18 },
});
