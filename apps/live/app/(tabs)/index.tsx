import React from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActivityRow, AppHeader, AvatarButton, Card, CollapsibleHeader, DocIcon,
  DottedRule, HouseArt, Screen, SectionTitle, StatTile, WrenchIcon,
  BedIcon, CalendarIcon, ChatIcon, ClipboardIcon, ShieldIcon,
  color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { currentInvoice, notices, receipts, tenancy, tickets, totalDue } from '../../src/data/mock';

export default function Home() {
  const router = useRouter();
  const open = tickets.filter((t) => t.status !== 'Resolved');

  return (
    <>
      <AppHeader
        title={tenancy.unit}
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        {/* the one thing a tenant opens the app for */}
        <Card style={{ marginTop: space(3) }}>
          <Text style={s.dueLabel}>Due in {currentInvoice.daysToDue} days</Text>
          <Text style={s.dueAmount}>{inr(totalDue)}</Text>
          <Text style={s.duePeriod}>{currentInvoice.period} · due {currentInvoice.dueOn}</Text>

          <Pressable style={s.payBtn} onPress={() => router.push('/(tabs)/pay')}>
            <Text style={s.payBtnText}>Pay now</Text>
          </Pressable>
          <Text style={s.payNote}>No charge to pay by UPI</Text>
        </Card>

        <Card padded={false} style={{ overflow: 'hidden' }}>
          <View style={s.art}><HouseArt size={150} /></View>
          <View style={{ padding: space(4) }}>
            <Text style={s.addr}>{tenancy.unit}</Text>
            <Text style={s.locality}>{tenancy.locality}</Text>
            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
              <StatTile value={tenancy.paidTo} label="rent paid to" tone="positive" />
              <StatTile value={tenancy.leaseExpires} label="lease expires" />
            </View>
            <View style={s.quickRow}>
              <Quick icon={<WrenchIcon size={20} />} label="Raise request" onPress={() => router.push('/raise')} />
              <Quick icon={<DocIcon size={20} />} label="Documents" onPress={() => router.push('/documents')} />
            </View>
            <View style={s.quickRow}>
              <Quick icon={<ShieldIcon size={20} />} label="Visitors" onPress={() => router.push('/visitors')} />
              <Quick icon={<ClipboardIcon size={20} />} label="Agreement" onPress={() => router.push('/tenancy')} />
            </View>
            <View style={s.quickRow}>
              <Quick icon={<BedIcon size={20} />} label="Book a service" onPress={() => router.push('/services')} />
              <Quick icon={<CalendarIcon size={20} />} label="Autopay" onPress={() => router.push('/autopay')} />
            </View>
          </View>
        </Card>

        <CollapsibleHeader title="Up Next" />
        {open.length ? (
          open.map((t) => (
            <Card key={t.id}>
              <View style={s.upTop}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }}>
                  <WrenchIcon size={19} c={color.positive} />
                  <Text style={s.status}>{t.status.toUpperCase()}</Text>
                </View>
                <Text style={s.liability}>
                  {t.liability === 'Tenant' ? 'You pay' : t.liability === 'Owner' ? 'Owner pays' : 'Shared'}
                </Text>
              </View>
              <DottedRule />
              <View style={{ paddingTop: space(3) }}>
                <Text style={s.upTitle}>{t.title}</Text>
                {t.slot ? <Text style={s.upMeta}>Visit {t.slot}</Text> : null}
                {t.vendor ? (
                  <View style={s.vendorChip}><Text style={s.vendorText}>{t.vendor}</Text></View>
                ) : null}
              </View>
            </Card>
          ))
        ) : null}

        <SectionTitle>Notices</SectionTitle>
        {notices.map((n) => (
          <ActivityRow
            key={n.id}
            icon={<CalendarIcon size={22} c={color.inkFaint} />}
            title={n.title}
            subtitle={<Text style={s.noticeBody}>{n.body}</Text>}
            meta={n.date}
          />
        ))}

        <CollapsibleHeader title="Recent" />
        {receipts.slice(0, 3).map((r) => (
          <ActivityRow
            key={r.id}
            icon={<ClipboardIcon size={22} c={color.inkFaint} />}
            title={`Rent paid — ${inr(r.paise)}`}
            subtitle={<Text style={s.recSub}>{r.period} · {r.method}</Text>}
            meta={r.date}
          />
        ))}
      </Screen>
    </>
  );
}

const Quick = ({ icon, label, onPress }: { icon: React.ReactNode; label: string; onPress: () => void }) => (
  <Pressable style={s.quick} onPress={onPress}>
    {icon}
    <Text style={s.quickText}>{label}</Text>
  </Pressable>
);

const s = StyleSheet.create({
  dueLabel: { ...font.label, color: color.inkSoft },
  dueAmount: { fontSize: 40, fontWeight: '800', color: color.inkStrong, letterSpacing: -0.8, marginTop: 4 },
  duePeriod: { ...font.body, color: color.inkSoft, marginTop: 4 },
  payBtn: {
    backgroundColor: color.accent, borderRadius: radius.pill,
    paddingVertical: space(4), alignItems: 'center', marginTop: space(4),
  },
  payBtnText: { ...font.h3, color: '#FFF' },
  payNote: { ...font.small, color: color.inkSoft, textAlign: 'center', marginTop: space(2) },

  art: { backgroundColor: '#EAF1FB', alignItems: 'center', paddingVertical: space(4) },
  addr: { ...font.h3, color: color.inkStrong, textAlign: 'center' },
  locality: { ...font.small, color: color.inkSoft, textAlign: 'center', marginTop: 3 },
  quickRow: { flexDirection: 'row', gap: 10, marginTop: space(3) },
  quick: {
    flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 7,
    backgroundColor: color.cardMuted, borderRadius: radius.md, paddingVertical: space(3),
  },
  quickText: { ...font.label, color: color.accent },

  upTop: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingBottom: space(3) },
  status: { ...font.tiny, color: color.positive, fontSize: 12.5 },
  liability: { ...font.small, color: color.inkSoft },
  upTitle: { ...font.h3, color: color.inkStrong },
  upMeta: { ...font.small, color: color.inkSoft, marginTop: 3 },
  vendorChip: {
    marginTop: space(3), backgroundColor: '#EEF3FC', borderRadius: radius.md,
    borderWidth: 1, borderColor: '#DCE5F5', paddingVertical: 11, alignItems: 'center',
  },
  vendorText: { ...font.label, color: color.segmentTo },

  noticeBody: { ...font.small, color: color.ink, marginTop: 3, lineHeight: 19 },
  recSub: { ...font.small, color: color.inkSoft, marginTop: 3 },
});
