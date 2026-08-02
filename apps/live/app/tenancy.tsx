import React, { useEffect, useState } from 'react';
import { Text, StyleSheet, View, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Screen, KeyValue, StatusPill, Segmented, Timeline, Toast,
  color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { fmtDate, serveNotice, useLiveData } from '../src/data/source';

const DAY = 24 * 3600 * 1000;
const iso = (t: number) => new Date(t).toISOString().slice(0, 10);

/**
 * Agreement, deposit and notice.
 *
 * A tenant's three anxious questions: what did I sign, where is my deposit,
 * and how do I leave. Each is answered from the lease record — and where the
 * record has no answer yet, the screen says so instead of inventing one.
 */

export default function Tenancy() {
  const router = useRouter();
  const [tab, setTab] = useState('Agreement');
  const { tenancy, loading } = useLiveData();
  const live = tenancy.state === 'active' || tenancy.state === 'in_notice';

  return (
    <>
      <BackHeader title="Your tenancy" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        <View style={{ marginTop: space(3), marginBottom: space(3) }}>
          <Segmented items={['Agreement', 'Deposit', 'Notice']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Agreement' ? (
          <Card>
            <StatusPill
              text={loading ? '…'
                : tenancy.state === 'in_notice' ? 'In notice'
                : tenancy.state ? tenancy.state.replace(/^\w/, (c) => c.toUpperCase())
                : 'Unknown'}
              tone={tenancy.state === 'in_notice' ? 'amber' : live ? 'green' : 'neutral'}
              dot
            />
            <Text style={s.h1}>Tenancy at {tenancy.unit}</Text>
            <Text style={s.sub}>{tenancy.locality}</Text>
            <View style={{ marginTop: space(3) }}>
              <KeyValue k="Term" v={tenancy.endOn ? `${tenancy.startOn} to ${tenancy.endOn}` : `From ${tenancy.startOn}, open-ended`} />
              <KeyValue k="Rent" v={`${inr(tenancy.rentPaise, { noPaise: true })} per month`} />
              <KeyValue k="Due day" v={`${tenancy.dueDay} of the month`} />
              {tenancy.lockInUntil ? <KeyValue k="Lock-in ends" v={tenancy.lockInUntil} /> : null}
              <KeyValue k="Notice period" v={`${tenancy.noticeDays} days`} />
              <KeyValue k="Managed by" v={tenancy.agency} last />
            </View>
            <Text style={s.note}>
              These are the terms as your landlord recorded them. If anything here does not match
              what you signed, raise it with your manager — the record, not memory, settles it.
            </Text>
          </Card>
        ) : null}

        {tab === 'Deposit' ? (
          <>
            <Card>
              <Text style={s.label}>Security deposit held</Text>
              <Text style={s.big}>{inr(tenancy.depositPaise)}</Text>
              <Text style={s.sub}>Held against the tenancy, as the ledger records it</Text>
              <Text style={s.note}>
                Your deposit is never set against rent without your written consent. Deductions at
                move-out must be itemised against the move-in condition report.
              </Text>
            </Card>
            <Card>
              <Text style={s.h}>How a refund is worked out</Text>
              <Timeline
                items={[
                  { at: 'Move-out day', what: 'Joint inspection against the move-in report' },
                  { at: 'Within 3 days', what: 'Itemised deductions shared with you, with photos' },
                  { at: 'You decide', what: 'Accept, or dispute it here' },
                  { at: 'Within 30 days', what: 'Balance refunded to your account', done: false },
                ]}
              />
            </Card>
          </>
        ) : null}

        {tab === 'Notice' ? <NoticeTab /> : null}
      </Screen>
    </>
  );
}

/** Serve notice to vacate (#239): pick a legal move-out day, review, serve.
 * Served means served — the manager is told the moment it posts. */
function NoticeTab() {
  const { tenancy, leaseId, loading } = useLiveData();
  const earliest = Date.now() + tenancy.noticeDays * DAY;
  const [moveOut, setMoveOut] = useState(earliest);
  const [reviewing, setReviewing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  // The default steers to penalty-free: past the lock-in when there is one.
  useEffect(() => {
    if (loading) return;
    const lockIn = tenancy.lockInUntilIso ? new Date(tenancy.lockInUntilIso).getTime() : 0;
    setMoveOut(Math.max(earliest, lockIn));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, tenancy.noticeDays, tenancy.lockInUntilIso]);

  if (tenancy.state === 'in_notice') {
    return (
      <Card>
        <StatusPill text="Notice served" tone="amber" dot />
        <Text style={s.h1}>You are moving out</Text>
        <View style={{ marginTop: space(3) }}>
          <KeyValue k="Notice served" v={tenancy.noticeServedOn} />
          <KeyValue k="Moving out on" v={tenancy.noticeMoveOutOn} last />
        </View>
        <Text style={s.note}>
          Rent accrues until your move-out day. Withdrawing notice is an agreement with your
          manager, not a button — talk to them and they record it.
        </Text>
      </Card>
    );
  }

  const inLockIn = !!tenancy.lockInUntilIso && moveOut < new Date(tenancy.lockInUntilIso).getTime();
  const step = (days: number) => setMoveOut((t) => Math.max(earliest, t + days * DAY));
  const serve = async () => {
    if (!leaseId || busy) return;
    setBusy(true);
    try {
      await serveNotice(leaseId, iso(moveOut));
      setReviewing(false);
    } catch (err) {
      setToast((err as Error).message);
      setTimeout(() => setToast(null), 3200);
    } finally {
      setBusy(false);
    }
  };

  if (reviewing) {
    return (
      <>
        {toast ? <Toast text={toast} /> : null}
        <Card>
          <Text style={s.h}>Review your notice</Text>
          <View style={{ marginTop: space(2) }}>
            <KeyValue k="Notice period" v={`${tenancy.noticeDays} days`} />
            <KeyValue k="Served" v="Today, the moment you confirm" />
            <KeyValue k="Moving out on" v={fmtDate(iso(moveOut))} tone={inLockIn ? 'red' : undefined} last />
          </View>
          {inLockIn ? (
            <Text style={s.warn}>
              This date is inside your lock-in ({tenancy.lockInUntil}). The lock-in rent still
              applies — your manager will show you the figure before settlement.
            </Text>
          ) : null}
          <Text style={s.note}>
            Serving notice tells your manager immediately and puts the tenancy in notice. Rent
            accrues until the move-out day.
          </Text>
        </Card>
        <View style={{ flexDirection: 'row', gap: 10, marginHorizontal: space(4) }}>
          <Button label="Back" tone="secondary" onPress={() => setReviewing(false)} style={{ flex: 1 }} />
          <Button label={busy ? 'Serving…' : 'Serve notice now'} onPress={serve} disabled={busy} style={{ flex: 1.6 }} />
        </View>
      </>
    );
  }

  return (
    <>
      {toast ? <Toast text={toast} /> : null}
      <Card>
        <Text style={s.h}>Leaving</Text>
        <Text style={s.body}>
          You must give {tenancy.noticeDays} days notice.
          {tenancy.lockInUntil
            ? ` Your lock-in ends on ${tenancy.lockInUntil} — leaving before that means the lock-in rent still applies.`
            : ''}
        </Text>
        <View style={{ marginTop: space(3) }}>
          <KeyValue k="Earliest legal move-out" v={fmtDate(iso(earliest))} />
          {tenancy.lockInUntil ? <KeyValue k="Earliest without penalty" v={tenancy.lockInUntil} /> : null}
          {tenancy.endOn ? <KeyValue k="Lease ends" v={tenancy.endOn} last /> : <KeyValue k="Term" v="Open-ended" last />}
        </View>
      </Card>
      <Card>
        <Text style={s.h}>Move out on</Text>
        <Text style={s.pickedDate}>{fmtDate(iso(moveOut))}</Text>
        {inLockIn ? <Text style={s.warn}>Inside your lock-in — the lock-in rent still applies.</Text> : null}
        <View style={s.stepRow}>
          <Step label="−7" onPress={() => step(-7)} />
          <Step label="−1" onPress={() => step(-1)} />
          <Step label="+1" onPress={() => step(1)} />
          <Step label="+7" onPress={() => step(7)} />
        </View>
        <Button
          label="Review notice"
          tone="secondary"
          onPress={() => setReviewing(true)}
          disabled={loading || !leaseId}
          style={{ marginTop: space(4) }}
        />
        <Text style={s.note}>
          Nothing is served until you confirm on the next screen.
        </Text>
      </Card>
    </>
  );
}

const Step = ({ label, onPress }: { label: string; onPress: () => void }) => (
  <Pressable style={s.step} onPress={onPress}>
    <Text style={s.stepText}>{label}</Text>
  </Pressable>
);

const s = StyleSheet.create({
  pickedDate: { fontSize: 26, fontWeight: '800', color: color.inkStrong, marginTop: space(2) },
  stepRow: { flexDirection: 'row', gap: 9, marginTop: space(3) },
  step: {
    flex: 1, alignItems: 'center', paddingVertical: space(3),
    borderRadius: radius.md, borderWidth: 1.4, borderColor: color.accent,
  },
  stepText: { ...font.label, color: color.accent },
  warn: { ...font.small, color: '#B4541B', marginTop: space(2), lineHeight: 18 },
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  label: { ...font.label, color: color.inkSoft },
  big: { fontSize: 32, fontWeight: '800', color: color.inkStrong, marginTop: 4 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
