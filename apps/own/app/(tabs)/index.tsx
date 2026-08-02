import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import { inr } from '../../src/data/mock';
import { useOwnerData } from '../../src/data/source';
import {
  ActivityRow,
  AppHeader,
  ListRow,
  StatusPill,
  AvatarButton,
  Badge,
  CalendarIcon,
  Card,
  ChatIcon,
  ChipRow,
  ClipboardIcon,
  CollapsibleHeader,
  DocIcon,
  EmptyState,
  HomeIcon,
  HouseArt,
  Screen,
  StatTile,
  WrenchIcon,
  color,
  font,
  radius,
  shadow,
  space,
  ui,
} from '@dwellm8/mobile-shared';

export default function Home() {
  const router = useRouter();
  const [filter, setFilter] = useState('All');
  const { loading, error, properties, statements, activities, approvals, upNext } = useOwnerData();
  const scope = properties[0];

  const showMaint = filter === 'All' || filter === 'Maintenance';
  const showInsp = filter === 'All' || filter === 'Inspections';
  const feed = activities.filter((a) =>
    a.kind === 'inspection' ? showInsp : showMaint,
  );

  if (loading || !scope) {
    return (
      <>
        <AppHeader title="Dwellm8" left={<AvatarButton onPress={() => router.push('/profile')} />} />
        <Screen>
          {loading ? (
            <View style={{ paddingTop: 120, alignItems: 'center' }}>
              <ActivityIndicator />
            </View>
          ) : (
            <EmptyState
              art={<HouseArt size={190} />}
              title={error ? 'Could not reach Dwellm8' : 'No properties yet'}
              body={error ?? 'Your portfolio appears here once a property is added.'}
            />
          )}
        </Screen>
      </>
    );
  }

  return (
    <>
      <AppHeader
        title={scope.address}
        onTitlePress={() => router.push('/switcher')}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        {/* property card */}
        <Card padded={false} style={{ overflow: 'hidden', marginTop: space(3) }}>
          {/* The art panel owns the card's whole top; the pill floats over it.
              The old negative-margin overlap left a white notch beside the
              pill whenever the pill outgrew the overlap. */}
          <View style={s.art}>
            <HouseArt size={168} />
          </View>
          <View style={s.agencyPill}>
            <Text style={s.agencyName}>{scope.agency}</Text>
            {scope.manager ? (
              <>
                <View style={s.agencyDivider} />
                <View style={s.pmDot}>
                  <Text style={s.pmDotText}>{scope.manager.split(' ').map((x) => x[0]).join('')}</Text>
                </View>
                <Text style={s.pmName}>{scope.manager}</Text>
              </>
            ) : null}
          </View>

          <View style={{ padding: space(4), paddingTop: space(3) }}>
            <Text style={s.addr}>{scope.address}</Text>
            <Text style={s.locality}>{scope.locality}</Text>

            <View style={s.quickRow}>
              <Quick icon={<ChatIcon size={20} />} label="Contact" onPress={() => router.push('/property?tab=Contact')} />
              <Quick icon={<HomeIcon size={20} />} label="Details" onPress={() => router.push('/property?tab=Details')} />
              <Quick icon={<DocIcon size={20} />} label="Documents" onPress={() => router.push('/property?tab=Documents')} />
              <Quick icon={<ChatIcon size={20} />} label="Applicants" onPress={() => router.push('/applications')} />
            </View>

            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
              <StatTile value={scope.paidTo} label="paid to date" tone="positive" />
              <StatTile value={scope.leaseExpires} label="lease expires" />
            </View>
          </View>
        </Card>

        <View style={s.dots}>
          {properties.map((p, i) => (
            <View key={p.id} style={[s.dot, i === 0 && s.dotActive]} />
          ))}
        </View>

        {/* Anything waiting on the owner outranks everything else on the screen. */}
        {approvals.length ? (
          <>
            <CollapsibleHeader title="Waiting on you" />
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {approvals.map((a, i) => (
                <ListRow
                  key={a.id}
                  title={`${a.title} — ${inr(a.quotePaise, { noPaise: true })}`}
                  subtitle={properties.find((p) => p.id === a.propertyId)?.address}
                  meta={`Raised ${a.raised} · ${a.vendor}`}
                  right={<StatusPill text={a.urgency} tone={a.urgency === 'Urgent' ? 'amber' : 'neutral'} />}
                  onPress={() => router.push(`/approve?id=${a.id}`)}
                  last={i === approvals.length - 1}
                  tone="violet"
                />
              ))}
            </Card>
          </>
        ) : null}

        <ChipRow
          items={[
            { label: 'All' },
            { label: 'Maintenance', icon: <WrenchIcon size={17} c={filter === 'Maintenance' ? '#FFF' : color.accent} /> },
            { label: 'Inspections', icon: <CalendarIcon size={17} c={filter === 'Inspections' ? '#FFF' : color.accent} /> },
          ]}
          value={filter}
          onChange={setFilter}
        />

        <CollapsibleHeader title="Up Next" />
        {showMaint && upNext ? (
          <Card>
            <View style={s.upNextTop}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }}>
                <WrenchIcon size={19} c={color.positive} />
                <Text style={s.inProgress}>{upNext.status}</Text>
              </View>
              <Text style={s.upNextProp} numberOfLines={1}>{upNext.property}</Text>
            </View>
            <View style={ui.dotted} />
            <View style={{ flexDirection: 'row', gap: 12, paddingTop: space(3) }}>
              <View style={{ width: 62, alignItems: 'center' }}>
                <CalendarIcon size={22} c={color.inkFaint} />
                <Text style={s.noDate}>No date</Text>
              </View>
              <View style={{ flex: 1 }}>
                <Text style={s.upNextTitle}>{upNext.title}</Text>
                <Text style={s.upNextMeta}>Reported {upNext.reported}</Text>
                <View style={s.vendorChip}>
                  <Text style={s.vendorText}>{upNext.vendor}</Text>
                </View>
              </View>
            </View>
          </Card>
        ) : (
          <EmptyState
            art={<HouseArt size={190} />}
            title="Everything's on track"
            body="No pending tasks. We'll notify you when there's something to review."
          />
        )}

        <CollapsibleHeader title="Recent Activities" />

        {filter === 'All'
          ? statements.slice(0, 2).map((st) => (
              <ActivityRow
                key={st.id}
                icon={<DocIcon size={22} c={color.inkFaint} />}
                title={`Statement ${st.n} — ${inr(st.netPaise)}`}
                subtitle={
                  <Text style={s.stSub}>
                    <Text style={{ color: color.positive }}>Income {inr(st.incomePaise)}</Text>
                    <Text style={{ color: color.inkFaint }}>{'  •  '}</Text>
                    <Text style={{ color: color.negative }}>Expense {inr(st.expensePaise)}</Text>
                  </Text>
                }
                meta={st.date}
              />
            ))
          : null}

        {feed.map((a) => (
          <ActivityRow
            key={a.id}
            icon={
              a.kind === 'inspection'
                ? <ClipboardIcon size={22} c={color.inkFaint} />
                : <WrenchIcon size={22} c={color.inkFaint} />
            }
            title={a.title}
            meta={a.date}
            onPress={() => router.push('/job')}
          />
        ))}
      </Screen>
    </>
  );
}

const Quick = ({ icon, label, onPress }: { icon: React.ReactNode; label: string; onPress: () => void }) => (
  <Pressable style={s.quick} onPress={onPress}>
    {icon}
    <Text style={s.quickText} numberOfLines={1}>{label}</Text>
  </Pressable>
);

const s = StyleSheet.create({
  agencyPill: {
    position: 'absolute', top: 0, left: 0,
    flexDirection: 'row', alignItems: 'center', gap: 9,
    backgroundColor: '#FFF',
    paddingRight: space(5), paddingLeft: space(4), paddingVertical: space(3),
    borderBottomRightRadius: radius.lg,
  },
  agencyName: { ...font.title, color: color.inkStrong },
  agencyDivider: { width: 1, height: 20, backgroundColor: color.line },
  pmDot: {
    width: 26, height: 26, borderRadius: 13, backgroundColor: '#DCE9F2',
    alignItems: 'center', justifyContent: 'center',
  },
  pmDotText: { ...font.tiny, color: color.accentDeep },
  pmName: { ...font.label, color: color.ink },

  art: { backgroundColor: '#EAF1FB', alignItems: 'center', paddingBottom: space(4), paddingTop: space(12) },

  addr: { ...font.h3, color: color.inkStrong, textAlign: 'center' },
  locality: { ...font.small, color: color.inkSoft, textAlign: 'center', marginTop: 3 },

  quickRow: { flexDirection: 'row', gap: 8, marginTop: space(4) },
  quick: {
    flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6,
    backgroundColor: color.cardMuted, borderRadius: radius.md, paddingVertical: space(3),
  },
  quickText: { ...font.label, color: color.accent },

  dots: { flexDirection: 'row', justifyContent: 'center', gap: 7, marginTop: 2, marginBottom: space(1) },
  dot: { width: 7, height: 7, borderRadius: 4, backgroundColor: '#C6D2E0' },
  dotActive: { backgroundColor: color.accentDeep },

  upNextTop: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingBottom: space(3), gap: 10 },
  inProgress: { ...font.tiny, color: color.positive, fontSize: 12.5 },
  upNextProp: { ...font.small, color: color.inkSoft, flexShrink: 1 },
  noDate: { ...font.small, color: color.inkFaint, marginTop: 3 },
  upNextTitle: { ...font.h3, color: color.inkStrong },
  upNextMeta: { ...font.small, color: color.inkSoft, marginTop: 3 },
  vendorChip: {
    marginTop: space(3), backgroundColor: '#EEF3FC', borderRadius: radius.md,
    borderWidth: 1, borderColor: '#DCE5F5', paddingVertical: 11, alignItems: 'center',
  },
  vendorText: { ...font.label, color: color.segmentTo },

  stSub: { ...font.small, marginTop: 3 },
});
