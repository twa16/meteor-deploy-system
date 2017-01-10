FROM debian:jessie

RUN apt-get update
RUN apt-get install -y sudo build-essential ruby-full rubygems git
RUN apt-get install -y "curl" "libmysqlclient-dev" "libpq-dev" "libsqlite3-0" "libevent-dev" "libssl-dev" "libxml2-dev" "libxslt1-dev" "libreadline-dev" "golang"
RUN gem install pkgr

#RUN mkdir /work
#WORKDIR /work
#RUN git clone https://github.com/twa16/meteor-deploy-system.git
#WORKDIR /work/meteor-deploy-system
#RUN pkgr package ./daemon

#CMD ["bash"]
