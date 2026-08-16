import { useQuery } from '@tanstack/react-query';
import { activityApi } from '../api/client';

const linkClass = 'text-indigo-600 underline underline-offset-2 hover:text-indigo-800';

function ExternalLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className={linkClass}>
      {children}
    </a>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-t border-gray-200 pt-7">
      <h2 className="text-xl font-semibold text-gray-900">{title}</h2>
      <div className="mt-3 space-y-3 leading-7 text-gray-600">{children}</div>
    </section>
  );
}

export default function AboutPage() {
  const { data } = useQuery({ queryKey: ['activity', 'policy'], queryFn: activityApi.policy });
  const retentionDays = data?.retention_days ?? 30;

  return (
    <article className="mx-auto max-w-3xl rounded-xl border border-gray-200 bg-white p-6 shadow-sm sm:p-10">
      <header>
        <p className="text-sm font-medium text-indigo-600">About seTORI</p>
        <h1 className="mt-1 text-3xl font-bold tracking-tight text-gray-900">このサイトについて</h1>
        <p className="mt-5 text-lg leading-8 text-gray-700">
          seTORI は、歌枠の中に残っている一曲一曲を、あとから探して、聴いて、
          好きな形でつなげるための非公式・非営利のファンサイトです。
        </p>
      </header>

      <div className="mt-9 space-y-9">
        <Section title="seTORI でできること">
          <p>
            曲、アーティスト、歌ったチャンネル、配信から歌唱を探し、公式 YouTube 動画の該当部分を
            続けて再生できます。気になった歌唱をキューや自分のプレイリストへ追加することもできます。
          </p>
          <p>
            曲名、歌った人、開始・終了時間などに間違いを見つけたときは、ログイン後の報告機能から
            修正を提案できます。みなさんからの提案を確認しながら、少しずつデータを整えています。
          </p>
        </Section>

        <Section title="非公式・非営利のファンサイトです">
          <p>
            seTORI は個人で開発・運営しているファンサイトです。掲載している配信者、所属事務所、
            YouTube、Holodex、Apple その他のサービスとは提携しておらず、公式サイトではありません。
          </p>
          <p>
            動画や音声を seTORI へ保存・再配布・ダウンロードすることはなく、再生には公式 YouTube の
            埋め込みプレーヤーを使います。動画、楽曲、画像、名称などの権利は、それぞれの権利者に帰属します。
            seTORI 自体に有料機能や独自の広告はありません。
          </p>
        </Section>

        <Section title="掲載データについて">
          <p>
            配信や楽曲の情報は、YouTube、Holodex、Apple の公開情報、配信の概要欄・公開コメント、
            運営者による確認、利用者からの修正提案などをもとに整理しています。
          </p>
          <p>
            自動処理や手作業を含むため、曲名、アーティスト、歌唱時間などが間違っていたり、
            更新が遅れたりすることがあります。情報の正確性や完全性を保証するものではありませんが、
            ご報告いただいた内容は確認して修正します。
          </p>
        </Section>

        <Section title="アカウントとプライバシー">
          <p>
            閲覧と再生はログインなしで利用できます。Google ログインは、プレイリストの保存、フォロー、
            修正提案など「誰のデータか」を区別する必要がある機能のために使います。
          </p>
          <ul className="list-disc space-y-2 pl-6">
            <li>
              Google から受け取るのは、アカウント識別子、メールアドレスと確認状態、表示名、
              プロフィール画像 URL です。Google のパスワードは取得せず、ログイン時のアクセストークンも保存しません。
            </li>
            <li>
              seTORI には、プレイリスト、フォロー、修正提案・報告とその確認結果が保存されます。
            </li>
            <li>
              アクセス時には、IP アドレス、ログイン中のアカウント、表示したページの pathname、日時・回数、
              User-Agent を記録します。不正利用への対応と利用状況の確認が目的です。
            </li>
            <li>
              アクセス記録は日単位でまとめ、原則 {retentionDays} 日後に削除します。完全な IP アドレスを
              確認できるのは、利用者管理権限を持つ運営者だけです。
            </li>
            <li>
              URL の query string、referrer、ページへ入力した内容はアクセス記録に保存しません。
            </li>
            <li>
              ブラウザの localStorage には、ログインを維持するセッショントークンとプレーヤーの音量設定を保存します。
              サーバー側にはセッショントークンそのものではなく、そのハッシュを保存します。
            </li>
          </ul>
          <p>
            これらの情報を販売したり、広告配信に使ったりすることはありません。アカウントや登録情報の削除を
            希望する場合は、下記の連絡先からお知らせください。障害復旧用バックアップには、ローテーションが
            終わるまで削除前の情報が一時的に残ることがあります。
          </p>
        </Section>

        <Section title="外部サービスについて">
          <p>
            本サイトでは、Google ログイン、YouTube API Services と埋め込みプレーヤー、Holodex、
            Apple の iTunes Search API／Apple Music、Cloudflare、ホスティングサービスを利用しています。
            公開コメントや楽曲・配信情報の整理に、運営者が設定した AI API を使う場合もありますが、
            アクセス記録や Google アカウント情報を AI API へ送ることはありません。
          </p>
          <p>
            YouTube API から取得するのは公開情報だけで、利用者の YouTube アカウントへのアクセス権限は要求しません。
            YouTube の利用には
            {' '}<ExternalLink href="https://www.youtube.com/t/terms">YouTube 利用規約</ExternalLink>
            {' '}が適用されます。Google による情報の取扱いは
            {' '}<ExternalLink href="https://policies.google.com/privacy">Google プライバシーポリシー</ExternalLink>
            {' '}をご確認ください。外部サービス側で Cookie や端末情報等が利用される場合があります。
          </p>
        </Section>

        <Section title="利用上のお願い">
          <p>気持ちよく使える場所を保つため、次のような利用はお控えください。</p>
          <ul className="list-disc space-y-2 pl-6">
            <li>第三者の権利やプライバシーを侵害する投稿、嫌がらせ、なりすまし、不正な報告</li>
            <li>不正アクセス、認証の回避、過度な自動取得など、サイトや他の利用者へ負荷・迷惑をかける行為</li>
            <li>YouTube その他の外部サービスが設ける再生や利用上の制限を回避する行為</li>
          </ul>
          <p>
            必要な場合は、投稿の非表示、機能の制限、セッションの失効やアカウントの停止を行うことがあります。
            また、個人運営のため、予告なく機能を変更・休止・終了する場合があります。
          </p>
        </Section>

        <Section title="掲載停止・修正・お問い合わせ">
          <p>
            掲載を希望されない配信者・権利者の方は、対象のチャンネルやページを添えてご連絡ください。
            ご本人または権利者であることを確認した上で、掲載停止や削除に対応します。データの間違いは、
            各ページの報告機能からもお知らせいただけます。
          </p>
          <p>
            その他のお問い合わせは
            {' '}<ExternalLink href="https://github.com/ruifan75/seTORI/issues">seTORI の GitHub Issues</ExternalLink>
            {' '}へお願いします。
          </p>
          <p className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900">
            GitHub Issues は公開されます。氏名、メールアドレス、IP アドレス、認証情報などを書かないでください。
            アカウント削除など非公開での対応が必要な場合は、詳細を書かずに「非公開での連絡を希望」とだけお知らせください。
          </p>
        </Section>
      </div>

      <footer className="mt-10 border-t border-gray-200 pt-5 text-xs text-gray-400">
        最終更新: 2026年8月16日
      </footer>
    </article>
  );
}
